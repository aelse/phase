// Package phase provides a context-aware mechanism to coordinate shutdown
// of subsystems in a deterministic, hierarchical order.
//
// A phase begins with a call to `phase.Next`, which takes a parent context and
// returns a `Phaser` and `error`.
// A `Phaser` implements `context.Context` and can be used anywhere a standard context is expected.
// If a non-nil error is returned the parent context or phase has ended. The function
// should clean up and return.
//
// The owner of a `Phaser` is responsible for coordinating its shutdown.
// This means waiting on child phases to terminate then calling `phaser.Close`.
//
// A phase ends when both of the following conditions are met:
//   - its context is canceled (as for other contexts, `<-p.Done()`)
//   - it has no remaining children (similar to above, `<-p.ChildrenDone()`)
//
// Once the phase has ended a function may perform any necessary cleanup and then call `p.Close()`
// to signal its own termination to a parent phase.
//
// Phase is able to discover parent phases through the context chain, even if intermediate code
// is not Phase aware. This means you can safely mix packages which do or don't support phases.
//
// # Example
//
//	func doSomething(ctx context.Context) {
//		ctx, cancel := context.WithTimeout(ctx, time.Second)
//		defer cancel()
//
//		ctx = phase.Next(ctx)
//		defer ctx.Close() // Ensure the phase ends
//
//		select {
//		case <-ctx.Done():
//			slog.Info("context ended")
//		case <-time.NewTicker(time.Second).C:
//			slog.Info("ticker fired")
//		}
//
//		// Wait for any child phases before returning
//		<-ctx.ChildrenDone()
//	}
//
// # Phase Lifecycle
//
// A phase is created by calling `phase.Next(ctx)`, which returns a Phaser context
// and an error value. A non-nil error indicates the phase cannot be started and
// the function should terminate. This can occur if the parent phase is waiting on
// children at the time the caller is attempting to create a new phase.
//
// The Phaser context can be passed freely to downstream functions as with any other
// `context.Context`. Additional phases created will register as child phases
// of the nearest ancestor `Phaser`.
//
// A phase ends with cancellation of the parent context. In this state the owning func
// should wait on its children to terminate, perform cleanup and then Close the Phaser.
//
// In order, the following occurs:
//  1. context cancellation is propagated immediately to all child phases
//  2. the phase owner waits for all children to complete
//  3. the phase owner marks its own phase as done, and returns
//
// The call to `phase.Close` is usually a deferred call.
package phase

import (
	"context"
	"errors"
	"sync"
)

// Next creates a new phase derived from the provided context.
// It returns a new Phaser, which implements context.Context.
//
// Calling Next is the starting point for phase coordination.
func Next(ctx context.Context) (phaser *phaseCtx, err error) {
	p := &phaseCtx{
		Context: ctx,
	}

	if err = p.init(ctx); err != nil {
		return nil, err
	}

	return p, nil
}

// A Phaser is a context.Context that participates in coordinated shutdown.
// It tracks child phases created via Next, and ensures they are all
// properly terminated before the phase itself is considered closed.
type Phaser interface {
	context.Context
	ChildrenDone() <-chan struct{}
	Close()
}

// phaseCtx meets Phaser and context.Context interfaces.
var _ Phaser = (*phaseCtx)(nil)
var _ context.Context = (*phaseCtx)(nil)

type phaseCtx struct {
	context.Context
	// parent is the nearest ancestor phaseCtx.
	parent *phaseCtx

	mu        sync.Mutex
	children  sync.WaitGroup
	beganWait bool
	chldDone  chan struct{}
}

var errParentWaitingOnChildren = errors.New("parent phaser is waiting on children")

func (p *phaseCtx) init(ctx context.Context) error {
	parent := ancestorPhaseCtx(ctx)
	if parent == nil {
		return nil
	}

	// If the parent is already waiting for children to terminate we do not register with it.
	// We do not rely on parent.Err() since it may end at any time outside of phaser control.
	parent.mu.Lock()
	if parent.beganWait {
		parent.mu.Unlock()

		return errParentWaitingOnChildren
	}

	parent.registerChild(p)
	parent.mu.Unlock()
	p.parent = parent

	return nil
}

// registerChild adds a child for ordered cancellation.
// May only be called when holding the mutex.
func (p *phaseCtx) registerChild(_ *phaseCtx) {
	p.children.Add(1)
}

// &ancestorCtxKey is the key that a phaseCtx returns itself for.
//
//nolint:gochecknoglobals
var ancestorCtxKey int

// ancestorPhaseCtx returns an ancestor *phaseCtx if there is one.
func ancestorPhaseCtx(ctx context.Context) *phaseCtx {
	if a, ok := ctx.Value(&ancestorCtxKey).(*phaseCtx); ok {
		return a
	}

	return nil
}

func (p *phaseCtx) Close() {
	// We only close once children have terminated.
	// We need to ensure no more children are created.
	p.mu.Lock()
	p.initChldDone()
	// Children need the mutex to deregister.
	p.mu.Unlock()
	p.children.Wait()

	p.mu.Lock()
	defer p.mu.Unlock()

	if p.parent != nil {
		p.parent.children.Done()
		// We must only do this once, so remove parent.
		p.parent = nil
	}
}

func (p *phaseCtx) ChildrenDone() <-chan struct{} {
	p.mu.Lock()
	p.initChldDone()
	p.mu.Unlock()

	return p.chldDone
}

// initChldDone marks that we have begun waiting on children, and creates a channel
// which is closed once no child phases remain.
// Must only be called when holding the mutex.
func (p *phaseCtx) initChldDone() {
	if !p.beganWait {
		p.beganWait = true
		p.chldDone = make(chan struct{})

		go func() {
			p.children.Wait()
			close(p.chldDone)
		}()
	}
}

func (p *phaseCtx) Value(key any) any {
	if key == &ancestorCtxKey {
		return p
	}

	return p.Context.Value(key)
}

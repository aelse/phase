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
// This includes ensuring all child phases have completed before calling `phase.Close`.
//
// A phase ends under either of the following conditions:
//   - its context is canceled (e.g., via `<-p.Done()`)
//   - `phase.Cancel(p)` is called explicitly
//
// In either case, before closing the phase, you **must** call `phase.Wait(p)`
// to ensure all child phases have completed.
//
// The following methods accept any `context.Context` as input:
//   - `phase.Next`
//   - `phase.Cancel`
//   - `phase.Close`
//   - `phase.Wait`
//
// These functions will automatically locate the nearest `Phaser` in the context
// chain. If none is found, the call panics, indicating a programming error.
//
// # Example
//
//	func doSomething(ctx context.Context) {
//		ctx, cancel := context.WithTimeout(ctx, time.Second)
//		defer cancel()
//
//		ctx = phase.Next(ctx)
//		defer phase.Close(ctx) // Ensure the phase ends
//
//		select {
//		case <-ctx.Done():
//			slog.Info("context ended")
//		case <-time.NewTicker(time.Second).C:
//			slog.Info("ticker fired")
//		}
//
//		// Wait for any child phases before returning
//		phase.Wait(ctx)
//	}
//
// # Phase Lifecycle
//
// A phase is created by calling `phase.Next(ctx)`, which returns a new context
// associated with the phase.
//
// That context can be passed freely to downstream functions. If those functions
// create their own phases using the same context, they will register as child phases
// of the nearest ancestor `Phaser`.
//
// A phase typically ends in response to one of:
//  1. a call to `phase.Cancel(ctx)`
//  2. cancellation of the parent context
//
// Upon termination, the following occurs:
//  1. cancellation is propagated to all child phases
//  2. the phase waits for all children to complete
//  3. the phase marks its own context as done
//
// The function owning the phase can use this opportunity to perform final cleanup
// before calling `phase.Close`, usually via a deferred call.
//
// ## A note on phase ownership
//
// A function may begin a phase to cover the scope of its own activities, or to
// hand off to another function. In the simple case where a function begins a phase
// for itself (ensuring function fully terminates before parent phases end) it is
// clear that the programmer must Close the phase before returning.
//
// When orchestrating the start of an application you
// may wish to create a number of phases and pass the relevant context to your
// application components (dependency injection) with the expectation the components
// will Close their phase before returning. If these phases are passed to those
// functions as a context.Context the requirement may be overlooked and so it
// is preferable to make the first argument to those functions a phase.Phaser
// to indicate the requirement and avoid confusion.
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
func Next(parent context.Context) (phaser *phaseCtx, err error) {
	ctx, cancel := context.WithCancel(parent)
	p := &phaseCtx{
		Context:    ctx,
		cancelFunc: cancel,
	}

	p.debug("calling init")
	err = p.init(ctx)

	if err != nil {
		return nil, err
	}

	return p, nil
}

// A Phaser is a context.Context that participates in coordinated shutdown.
//
// A Phaser tracks child phases created via Next, and ensures they are all
// properly terminated before the phase itself is considered closed.
type Phaser interface {
	context.Context
	Cancel()
	Close()
	WaitForChildren()
}

// phaseCtx meets Phaser and context.Context interfaces.
var _ Phaser = (*phaseCtx)(nil)
var _ context.Context = (*phaseCtx)(nil)

type phaseCtx struct {
	context.Context
	cancelFunc context.CancelFunc
	// parent is the nearest ancestor phaseCtx.
	parent *phaseCtx

	mu        sync.Mutex
	children  sync.WaitGroup
	beganWait bool
}

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
		return errors.New("parent phaser is waiting on children")
	}

	p.debug("adding self to parent's children")
	parent.registerChild(p)
	parent.mu.Unlock()
	p.parent = parent
	p.debug("unlocked parent")

	return nil
}

// registerChild adds a child for ordered cancellation.
// May only be called when holding the mutex.
func (p *phaseCtx) registerChild(_ *phaseCtx) {
	p.debug("registering child")
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

func (p *phaseCtx) debug(_ string) {
	// fmt.Printf("%p: %s\n", p, message)
}

func (p *phaseCtx) Cancel() {
	p.cancelFunc()
}

func (p *phaseCtx) Close() {
	// Always call cancel in case it has not been called already, to close our internal context.
	// This is the case when the context ends due to propagated context cancelation.
	p.cancelFunc()

	p.mu.Lock()
	defer p.mu.Unlock()

	if p.parent != nil {
		p.debug("telling parent")
		p.parent.children.Done()
		// We must only do this once, so remove parent.
		p.parent = nil
	}
}

func (p *phaseCtx) WaitForChildren() {
	// Mark that we have begun waiting and can no longer have children added.
	p.mu.Lock()
	p.beganWait = true
	p.mu.Unlock()

	p.debug("waiting on children")
	p.children.Wait()
	p.debug("finished waiting on children")
}

func (p *phaseCtx) Value(key any) any {
	if key == &ancestorCtxKey {
		return p
	}

	return p.Context.Value(key)
}

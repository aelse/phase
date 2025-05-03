// Package phase provides a context-aware mechanism to coordinate shutdown
// of subsystems in a deterministic, hierarchical order.
//
// A phase begins with a call to `phase.Next`, which takes a parent context and
// returns a `Phaser`. A `Phaser` implements `context.Context` and can be used
// anywhere a standard context is expected.
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
package phase

import (
	"context"
	"sync"
)

// Next creates a new phase derived from the provided context.
// It returns a new Phaser, which implements context.Context.
//
// Calling Next is the starting point for phase coordination.
func Next(parent context.Context) (phaser Phaser) {
	ctx, cancel := context.WithCancel(parent)
	p := &phaseCtx{
		Context:    ctx,
		cancelFunc: cancel,
	}
	p.debug("calling init")
	p.init(ctx)

	return p
}

// Cancel signals that the phase identified by the given context should end.
// This cancels the context and begins coordinated shutdown.
//
// Cancel will panic if the context does not contain a Phaser.
func Cancel(ctx context.Context) {
	phase := ancestorPhaseCtx(ctx)
	if phase == nil {
		panic("attempt to cancel phase when not in a phase")
	}

	phase.close()
}

// Close marks the phase as complete and removes it from its parent.
// This should be called after all child phases have completed, typically
// after a call to Wait.
//
// Close will panic if the context does not contain a Phaser.
func Close(ctx context.Context) {
	phase := ancestorPhaseCtx(ctx)
	if phase == nil {
		panic("attempt to close phase when not in a phase")
	}

	phase.close()
}

// Wait blocks until all child phases of the identified phase have completed.
// It must be called before Close to ensure all nested work is finished.
//
// Wait will panic if the context does not contain a Phaser.
func Wait(ctx context.Context) {
	phase := ancestorPhaseCtx(ctx)
	if phase == nil {
		panic("attempt to wait when not in a phase")
	}

	phase.wait()
}

// A Phaser is a context.Context that participates in coordinated phase shutdown.
//
// A Phaser tracks child phases created via Next, and ensures they are all
// properly terminated before the phase itself is considered closed.
type Phaser interface {
	context.Context
	cancel()
	close()
	wait()
}

var _ Phaser = &phaseCtx{}

type phaseCtx struct {
	context.Context
	cancelFunc context.CancelFunc
	// parent is the nearest ancestor phaseCtx.
	parent *phaseCtx

	mu       sync.Mutex
	children sync.WaitGroup
}

func (p *phaseCtx) init(ctx context.Context) {
	// Keep parent context which we need for calls to Value, and tell func for notifying
	// parent phaseCtx when we terminate. We call TryLock to try to avoid lengthy blocking
	// if the parent is being cancelled.
	if parent := ancestorPhaseCtx(ctx); parent != nil {
		// If the parent has already propagated cancellation to children we do not register with it.
		// That cancellation *should* trickle down through any intermediary contexts here, and the user
		// of this context will see the results in calling Done() or Err() on us.
		// The cancellation from the ancestor may not propagate if an intermediary context has prevented it.
		// eg. using context.WithoutCancel. That's out of our hands.
		parent.mu.Lock()
		if parent.Err() == nil {
			p.debug("adding self to parent's children")
			parent.registerChild(p)
		}
		parent.mu.Unlock()
		p.parent = parent
		p.debug("unlocked parent")
	}
}

// registerChild adds a child for ordered cancellation.
// May only be called when holding the mutex.
func (p *phaseCtx) registerChild(_ *phaseCtx) {
	if p.Err() != nil {
		p.debug("skipping registration")

		return
	}

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

func (p *phaseCtx) cancel() {
	p.cancelFunc()
}

func (p *phaseCtx) close() {
	// Always call cancel in case it has not been called already, to close our internal context.
	// This is the case when the context ends due to propagated context cancelation.
	p.cancelFunc()

	p.mu.Lock()
	defer p.mu.Unlock()

	if p.parent != nil {
		p.debug("telling parent")
		p.parent.children.Done()
		p.parent = nil
	}
}

func (p *phaseCtx) wait() {
	p.debug("waiting on children")
	p.children.Wait()
}

func (p *phaseCtx) Value(key any) any {
	if key == &ancestorCtxKey {
		return p
	}

	return p.Context.Value(key)
}

package phase

import (
	"context"
	"sync"
	"time"
)

// Phaser is a Context providing ordered shutdown.
type Phaser interface {
	// Phaser is a Context and may be passed to functions requiring a Context.
	context.Context

	// Next registers and returns a new child Phaser. This should be called to
	// create a new Phaser for each downstream component that needs ordered shutdown.
	Next() Phaser

	// When the child Phaser context has finished (context semantics, the Done() channel),
	// Cancel must be called to signal the parent and allow ordered shutdown to continue.
	Cancel()
}

func FromContext(ctx context.Context) *phaserImpl {
	phaser := &phaserImpl{}
	phaser.init(ctx)
	return phaser
}

type phaserImpl struct {
	pctx       context.Context
	ctx        context.Context
	cancel     context.CancelFunc
	dcancel    context.CancelFunc
	chldCtx    context.Context
	chldCancel context.CancelFunc
	cancelOnce sync.Once
	tellParent func()
	children   sync.WaitGroup
}

func (p *phaserImpl) init(ctx context.Context) {
	// Keep parent context which we need for calls to Value.
	p.pctx = ctx
	// Create a new cancelable context for ourselves, also used for Done and Err.
	// This decouples cancellation from upstream context.
	ctx2, cancel := context.WithCancel(context.Background())
	// Copy the deadline if one is set on the original context.
	if deadline, ok := ctx.Deadline(); ok {
		var dcancel context.CancelFunc
		ctx2, dcancel = context.WithDeadline(ctx2, deadline)
		p.dcancel = dcancel
	}
	p.ctx, p.cancel = ctx2, cancel
	// Create a cancellable context for children.
	chldCtx, chldCancel := context.WithCancel(ctx2)
	p.chldCtx, p.chldCancel = chldCtx, chldCancel

	// When parent ctx ends we cancel all downstream Phasers and then our own context.
	// This preserves ordering in that all children terminate before our context ends.
	go func() {
		<-p.pctx.Done()
		p.doCancel()
	}()
}

func (p *phaserImpl) Next() Phaser {
	phaser := &phaserImpl{}
	phaser.init(p.chldCtx)
	p.children.Add(1)
	phaser.tellParent = p.children.Done
	return phaser
}

// Cancel triggers cancellation of the Phaser chain. This must be called when Phaser context
// has finished, and may be called to trigger cancellation of downstream phasers.
func (p *phaserImpl) Cancel() {
	p.cancelOnce.Do(func() {
		p.doCancel()
		// Once our context is closed (after children terminate), notify parent.
		if p.tellParent != nil {
			go func() {
				<-p.Done()
				// Parent is notified when downstream phasers and this context have finished.
				p.tellParent()
			}()
		}
	})
}

func (p *phaserImpl) doCancel() {
	// Immediately cancel child contexts to trigger downstream effects.
	p.chldCancel()
	// Wait in a goroutine for children to terminate, to avoid blocking.
	go func() {
		p.children.Wait()
		// Once children have terminated we can cancel our own context.
		p.cancel()
	}()
}

// Implement Context by wraping calls to context objects.
// Value point to the upstream context. Everything else to our new context.

func (p *phaserImpl) Done() <-chan struct{} {
	return p.ctx.Done()
}

func (p *phaserImpl) Deadline() (deadline time.Time, ok bool) {
	return p.ctx.Deadline()
}

func (p *phaserImpl) Err() error {
	return p.ctx.Err()
}

func (p *phaserImpl) Value(key interface{}) interface{} {
	return p.pctx.Value(key)
}

package phase

import (
	"context"
	"sync"
	"time"
)

func FromContext(ctx context.Context) *Phaser {
	// TODO: check if ctx is a Phaser
	phaser := &Phaser{}
	phaser.init(ctx, nil)
	return phaser
}

// New returns a Phaser
func New() *Phaser {
	return FromContext(context.Background())
}

type Phaser struct {
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

func (p *Phaser) init(ctx context.Context, tell func()) {
	// Keep parent context which we need for calls to Value, and tell func for notifying
	// parent Phaser when we terminate.
	p.pctx = ctx
	p.tellParent = tell
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

	go func() {
		select {
		// When parent ctx ends we cancel all downstream Phasers and then our own context.
		// This preserves ordering in that all children terminate before our context ends.
		case <-p.pctx.Done():
			p.doCancel()
		// If our context is cancelled (but not parent ctx) we return to avoid leaking the goroutine.
		case <-p.Done():
			break
		}
	}()
}

// Next registers and returns a new child Phaser. This should be called to
// create a new Phaser for each downstream component that needs ordered shutdown.
func (p *Phaser) Next() *Phaser {
	p.children.Add(1)
	phaser := &Phaser{}
	phaser.init(p.chldCtx, p.children.Done)
	return phaser
}

// Cancel triggers cancellation of the Phaser chain. This must be called when Phaser context
// has finished (context semantics, the Done() channel),, and may be called to trigger
// cancellation of downstream phasers.
func (p *Phaser) Cancel() {
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

func (p *Phaser) doCancel() {
	// Immediately cancel child contexts to trigger downstream effects.
	p.chldCancel()
	// Wait in a goroutine for children to terminate, to avoid blocking.
	go func() {
		p.children.Wait()
		// Once children have terminated we can cancel our own context.
		p.cancel()
	}()
}

// Implement Context by wrapping calls to context objects.
// Value point to the upstream context. Everything else to our new context.

func (p *Phaser) Done() <-chan struct{} {
	return p.ctx.Done()
}

func (p *Phaser) Deadline() (deadline time.Time, ok bool) {
	return p.ctx.Deadline()
}

func (p *Phaser) Err() error {
	// TODO: handle case where phaser cancelled but parent context not
	return p.ctx.Err()
}

func (p *Phaser) Value(key interface{}) interface{} {
	return p.pctx.Value(key)
}

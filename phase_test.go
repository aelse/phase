package phase

import (
	"context"
	"testing"
	"time"
)

func assertContextAlive(t *testing.T, ctx context.Context) {
	t.Helper()
	select {
	case <-ctx.Done():
		t.Errorf("Expected context to be alive")
	default:
	}
}

func assertContextFinished(t *testing.T, ctx context.Context) {
	t.Helper()
	select {
	case <-ctx.Done():
	default:
		t.Errorf("Expected context to be finished")
	}
}

func TestPhaseCancelHeirarchy(t *testing.T) {
	p0 := FromContext(context.Background())
	p00 := p0.Next()
	p01 := p0.Next()
	p010 := p01.Next()
	p011 := p01.Next()

	// At the start nothing has terminated, and set up a Cancel trigger upon context end.
	for _, phaser := range []*Phaser{p0, p00, p01, p010, p011} {
		assertContextAlive(t, phaser)
		p := phaser
		go func() {
			<-p.Done()
			p.Cancel()
		}()
	}

	// Cancel p01 phaser, which should also cancel p010 and p011 but nothing else.
	p01.Cancel()

	// Allow time for goroutines to run.
	time.Sleep(10 * time.Millisecond)

	for _, ctx := range []*Phaser{p0, p00} {
		assertContextAlive(t, ctx)
	}
	for _, ctx := range []*Phaser{p01, p010, p011} {
		assertContextFinished(t, ctx)
	}

	// Cancel p0 and everything should end.
	p0.Cancel()
	time.Sleep(10 * time.Millisecond)
	for _, phaser := range []*Phaser{p0, p00, p01, p010, p011} {
		assertContextFinished(t, phaser)
	}
}

func TestPhaseChainedCancel(t *testing.T) {
	p0 := FromContext(context.Background())
	px := p0
	for i := 0; i < 10; i++ {
		px = px.Next()
		p := px // loop scope variable
		go func() {
			<-p.Done()
			p.Cancel()
		}()
	}

	assertContextAlive(t, p0)
	assertContextAlive(t, px)

	// Cancel p0 context, which cancels the entire chain.
	p0.Cancel()

	// Allow time for goroutines to run.
	time.Sleep(time.Millisecond)

	assertContextFinished(t, p0)
	assertContextFinished(t, px)
}

func TestPhaseCancelCascade(t *testing.T) {
	// When a Phaser's upstream context ends it should cancel downstream elements before its own context.
	// Any listeners to the Phaser's Done() channel should not fire until after all children have done the same.
	ctx, cancel := context.WithCancel(context.Background())
	results := make(chan string, 2)

	p0 := FromContext(ctx)
	go func() {
		defer p0.Cancel()
		<-p0.Done()
		results <- "p0"
	}()

	p1 := p0.Next()
	go func() {
		defer p1.Cancel()
		<-p1.Done()
		// Parent Phaser's context should not end until we send a notification via Cancel()
		time.Sleep(10 * time.Millisecond)
		results <- "p1"
	}()

	// Cancel the root context to trigger the cascade.
	cancel()
	// The first result should always come from p1 since its goroutine
	v := <-results
	if v != "p1" {
		t.Errorf("Expected child context (p1) termination but got %s", v)
	}
	v = <-results
	if v != "p0" {
		t.Errorf("Expected parent context (p0) termination but got %s", v)
	}
}

func TestPhaseValue(t *testing.T) {
	ctx := context.WithValue(context.Background(), "test", "test")
	p0 := FromContext(ctx)
	if p0.Value("test") != "test" {
		t.Errorf("Did not get expected value from context")
	}
}

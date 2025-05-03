package phase_test

import (
	"context"
	"testing"
	"time"

	"github.com/aelse/phase"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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

func TestPhaseCloseOne(t *testing.T) {
	t.Parallel()

	p := phase.Next(context.Background())
	phase.Wait(p)
	phase.Close(p)
	assert.Error(t, p.Err(), "expect context to return error")
}

func TestPhaseCancel(t *testing.T) {
	t.Parallel()

	p := phase.Next(context.Background())
	phase.Cancel(p)
	assert.NotPanics(t, func() { phase.Cancel(p) }, "additional calls to cancel do not cause panic")
}

func TestDeadlineInParent(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithDeadline(context.Background(), time.Now().Add(10*time.Millisecond))
	defer cancel()

	p0 := phase.Next(ctx)
	defer phase.Close(p0)
	// Expect not to block since deadline cancels context chain.
	<-p0.Done()
	assert.Error(t, p0.Err())
}

func TestPhaseCancelChild(t *testing.T) {
	t.Parallel()

	p0 := phase.Next(context.Background())
	defer phase.Close(p0) // cleanup

	ctx, cancel := context.WithCancel(p0)
	p1 := phase.Next(ctx)

	// Cancel child context
	cancel()
	t.Logf("cancelled child")

	// expect not to timeout due to blocking
	<-p1.Done()
	require.Error(t, p1.Err(), "expect child context returns error")
	require.NoError(t, p0.Err(), "expect parent context is alive")
}

func TestPhaseCancelParent(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())

	p0 := phase.Next(ctx)
	defer phase.Close(p0)

	p1 := phase.Next(p0)

	// Child is responsible for calling Done when its context ends.
	go func() {
		<-p1.Done()
		phase.Wait(p1)
		phase.Close(p1)
	}()

	// Cancel top level context, which should propagate down.
	cancel()

	// Expect test not to timeout due to blocking on either context.
	<-p0.Done()
	<-p1.Done()
	require.Error(t, p0.Err())
	require.Error(t, p1.Err())
}

func TestPhaseCancelHeirarchy(t *testing.T) {
	t.Skip()
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	p0 := phase.Next(ctx)
	p00 := phase.Next(p0)
	p01 := phase.Next(p0)
	p010 := phase.Next(p01)
	p011 := phase.Next(p01)

	// At the start nothing has terminated, and set up a Cancel trigger upon context end.
	for _, p := range []context.Context{p0, p00, p01, p010, p011} {
		assertContextAlive(t, p)

		go func(p context.Context) {
			<-p.Done()
			phase.Wait(p)
			phase.Close(p)
		}(p)
	}

	// Cancel p01 phaser, which should also cancel p010 and p011 but nothing else.
	phase.Close(p01)

	// Allow time for goroutines to run.
	time.Sleep(10 * time.Millisecond)

	for _, ctx := range []context.Context{p0, p00} {
		assertContextAlive(t, ctx)
	}

	for _, ctx := range []context.Context{p01, p010, p011} {
		assertContextFinished(t, ctx)
	}

	// Cancel top level context and everything should end.
	cancel()
	time.Sleep(10 * time.Millisecond)

	for _, p := range []context.Context{p0, p00, p01, p010, p011} {
		assertContextFinished(t, p)
	}
}

func TestPhaseChainedCancel(t *testing.T) {
	t.Parallel()

	ctx0, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()

	ctx := ctx0
	for i := range 10 {
		ctx = phase.Next(ctx)

		go func(ctx context.Context, i int) {
			t.Logf("%d: waiting on context cancellation", i)
			<-ctx.Done()
			// If the context ends we wait on children
			t.Logf("%d: waiting on children", i)
			phase.Wait(ctx)
			// and then signal parent we're done
			t.Logf("%d: signal parent", i)
			phase.Close(ctx)
		}(ctx, i)
	}

	assertContextAlive(t, ctx0)
	assertContextAlive(t, ctx)

	// Allow time for goroutines to run after top level context times out.
	time.Sleep(100 * time.Millisecond)

	assertContextFinished(t, ctx0)
	assertContextFinished(t, ctx)
}

func TestPhaseCancelCascade(t *testing.T) {
	t.Parallel()

	// When a Phaser's upstream context ends it should cancel downstream elements before its own context.
	// Any listeners to the Phaser's Done() channel should not fire until after all children have done the same.
	ctx, cancel := context.WithCancel(context.Background())
	results := make(chan string, 2)

	p0 := phase.Next(ctx)
	go func() {
		<-p0.Done()
		phase.Wait(p0)
		results <- "p0"

		phase.Close(p0)
	}()

	p1 := phase.Next(p0)
	go func() {
		<-p1.Done()
		// Parent Phaser's context should not end until we send a notification via Cancel()
		time.Sleep(10 * time.Millisecond)
		phase.Wait(p1)
		results <- "p1"

		phase.Close(p1)
	}()

	// Cancel the root context to trigger the cascade.
	cancel()
	// The first result should always come from p1 since p0 should block.
	v := <-results
	if v != "p1" {
		t.Errorf("Expected child context (p1) termination but got %s", v)
	}

	v2 := <-results
	if v2 != "p0" {
		t.Errorf("Expected parent context (p0) termination but got %s", v2)
	}
}

func TestPhaseValue(t *testing.T) {
	t.Parallel()

	ctx := context.WithValue(context.Background(), &t, "test")

	p0 := phase.Next(ctx)
	defer phase.Close(p0)

	assert.Equal(t, "test", p0.Value(&t), "Expected value to propagate through phase context")
}

func TestPhaserErrWhenParentCtxCancelled(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())

	p0 := phase.Next(ctx)
	defer phase.Close(p0)

	cancel()
	<-p0.Done()
	assert.Error(t, p0.Err(), "Expected error after cancellation")
}

func TestPhaserWaitBlocksUntilDone(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())

	p0 := phase.Next(ctx)
	defer phase.Close(p0)

	cancel()
	select {
	case <-p0.Done():
		t.Log("Waited for done")
	default:
		t.Error("Expected Cancel() to block until context was done")
	}
	assert.Error(t, p0.Err(), "Expected non nil error after cancellation")
}

func TestPhaserContextCancelPropagatesToPhaser(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())

	p0 := phase.Next(ctx)
	defer phase.Close(p0)

	cancel()
	select {
	case <-p0.Done():
		t.Log("Waited for done and found it")
	default:
		t.Error("Expected Cancel to terminate phaser context")
	}
	assert.Error(t, p0.Err(), "Expected non nil error after cancellation")
}

func TestPhaserSelfCancel(t *testing.T) {
	t.Parallel()

	p0 := phase.Next(context.Background())
	defer phase.Close(p0)

	phase.Cancel(p0)
	select {
	case <-p0.Done():
		t.Log("Waited for done and found it")
	default:
		t.Error("Expected Cancel to terminate phaser context")
	}
	assert.Error(t, p0.Err(), "Expected non nil error after cancellation")
}

func TestPhaserSelfCancelViaContextChain(t *testing.T) {
	t.Parallel()

	p0 := phase.Next(context.Background())
	defer phase.Close(p0)

	ctx := context.WithValue(p0, &t, "this just sets an intermediary context")

	phase.Cancel(ctx)
	select {
	case <-p0.Done():
		t.Log("Waited for done and found it")
	default:
		t.Error("Expected Cancel to terminate phaser context")
	}
	assert.Error(t, p0.Err(), "Expected non nil error after cancellation")
}

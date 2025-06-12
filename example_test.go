package phase_test

import (
	"context"
	"fmt"
	"time"

	"github.com/aelse/phase"
)

func ExamplePhaser() {
	f := func(ctx context.Context) {
		next, _ := phase.Next(ctx)
		select {
		case <-next.Done():
			fmt.Println("context ended")
		case <-time.NewTicker(10 * time.Millisecond).C:
			fmt.Println("ticker fired")
		}
		// Wait until child phases end.
		// There are none in this example but we demonstrate correct behaviour.
		next.WaitForChildren()
		// Signal that our phase has ended.
		next.Close()
	}

	ctx := context.Background()
	f(ctx)
	ctx, cancel := context.WithCancel(ctx)
	cancel()
	f(ctx)
	// Output: ticker fired
	// context ended
}

func ExamplePhaser_orderedShutdown() {
	f := func(p phase.Phaser, message string) {
		defer p.Close()
		<-p.Done()
		p.WaitForChildren()
		fmt.Println(message)
	}

	ctx, cancel := context.WithCancel(context.Background())

	phaser, _ := phase.Next(ctx)
	defer phaser.Close()

	p0, _ := phase.Next(phaser)
	p1, _ := phase.Next(p0)
	p2, _ := phase.Next(p1)

	go f(p0, "p0 ended")
	go f(p1, "p1 ended")
	go f(p2, "p2 ended")

	// We expect contexts to end in order: p2, p1, p0.

	// Cancel Phaser chain
	cancel()
	phaser.WaitForChildren()
	fmt.Println("finished!")
	// Output: p2 ended
	// p1 ended
	// p0 ended
	// finished!
}

func ExamplePhaser_funcTree() {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	phaser, _ := phase.Next(ctx)
	defer phaser.Close()

	type recursiveFunc func(f recursiveFunc, ctx context.Context, i, depth int)

	f := func(f recursiveFunc, ctx context.Context, i, depth int) {
		if i >= depth {
			return
		}
		// Create a phase to cover this function scope, and defer Close.
		p, _ := phase.Next(ctx)
		defer p.Close()

		// Create child funcs up to depth.
		go f(f, p, i+1, depth)

		// Wait on phase to be done, either due to propagated cancelation
		// or a call to phase.Cancel targeting `p` (n/a in this example).
		fmt.Printf("func<%d> doing work, waiting for context end\n", i)
		<-p.Done()
		// Child phases must return first
		p.WaitForChildren()
		fmt.Printf("func<%d> returning\n", i)
	}

	go f(f, phaser, 0, 5)
	// Top level context timeout propagates down and we wait on the phaser.
	<-ctx.Done()
	fmt.Println("Waiting on top level phaser to complete")
	phaser.WaitForChildren()
	fmt.Println("Top level phaser ended. Bye!")
}

func ExamplePhaser_contexts() {
	// Create a top lever phaser which will be cancelled at end of main.
	ctx, cancel := context.WithCancel(context.Background())
	p0, _ := phase.Next(ctx)
	defer p0.Close()

	go func(ctx context.Context) {
		fmt.Println("started p0 func")

		p1, _ := phase.Next(ctx)
		defer p1.Close()

		type ctxStr string
		// Run some other goroutines which take an ordinary context.
		for i := range 5 {
			// Phasers can be used like any other context. Let's set a value.
			ctx := context.WithValue(p1, ctxStr("goroutine"), i)
			go func(ctx context.Context) {
				//nolint:forcetypeassert
				num := ctx.Value(ctxStr("goroutine")).(int)
				fmt.Printf("goroutine(%d) started\n", num)
				<-ctx.Done()
				fmt.Printf("goroutine(%d) finished\n", num)
			}(ctx)
		}

		// Wait for our initiating context cancellation to perform cleanup.
		<-ctx.Done()

		fmt.Println("Waiting for descendent phases to end (none in this example)")
		p1.WaitForChildren()

		// There is no guarantee on order of completion for the goroutines
		// as they only deal in contexts not Phasers. I can try to manage this
		// in some other way.
		fmt.Println("Waiting a few seconds for goroutines to finish")
		time.Sleep(3 * time.Second)
	}(p0)

	fmt.Println("Shutdown in 5 seconds")
	time.Sleep(5 * time.Second)
	cancel()
	fmt.Println("Waiting on children")
	p0.WaitForChildren() // Wait until everything has finished.
	fmt.Println("Bye!")
}

func ExamplePhaser_phaserDI() {
	// Simulate any component that needs to perform cleanup at end of context.
	// The first are is a phase.Phaser rather than context.Context so that the
	// programmer knows to deal with handling the phase.
	// This is a hint to the programmer, not a difference in implementation.
	component := func(phaser phase.Phaser, name string) {
		defer phaser.Close()
		fmt.Printf("%s started\n", name)
		<-phaser.Done()
		// There might be child phases, so call Wait
		phaser.WaitForChildren()
		fmt.Printf("%s shutting down\n", name)
		time.Sleep(time.Second)
	}

	// Create a top level phase which will be cancelled at end of main.
	ctx, cancel := context.WithCancel(context.Background())
	p0, _ := phase.Next(ctx)
	defer p0.Close()

	// Create a tree of phasers from the root and use dependency injection to pass it
	// to each component.
	p1, _ := phase.Next(p0)
	go component(p1, "db")

	p2, _ := phase.Next(p1)
	go component(p2, "data pipeline")

	p3, _ := phase.Next(p2)
	go component(p3, "web server")

	fmt.Println("Shutdown in 2 seconds")
	time.Sleep(2 * time.Second)

	// Cancel this context, which cascades down.
	cancel()
	// Wait until everything has finished.
	p0.WaitForChildren()
	fmt.Println("Bye!")
}

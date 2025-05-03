package phase_test

import (
	"context"
	"fmt"
	"time"

	"github.com/aelse/phase"
)

func ExamplePhaser() {
	f := func(ctx context.Context) {
		next := phase.Next(ctx)
		select {
		case <-next.Done():
			fmt.Println("context ended")
		case <-time.NewTicker(10 * time.Millisecond).C:
			fmt.Println("ticker fired")
		}
		// Wait until child phases end.
		// There are none in this example but we demonstrate correct behaviour.
		phase.Wait(next)
		// Signal that our phase has ended.
		phase.Close(next)
	}

	ctx := context.Background()
	f(ctx)
	ctx, cancel := context.WithCancel(ctx)
	cancel()
	f(ctx)
	// Output: ticker fired
	// context ended
}

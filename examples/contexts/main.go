package main

// My application consists of code I own and other libraries which take a context.
// I'd like to control ordered shutdown of my subsytems and leverage a Phaser
// to manage 3rd party library interactions as if it is a normal context.

import (
	"context"
	"fmt"
	"time"

	"github.com/aelse/phase"
)

func main() {
	// Create a top lever phaser which will be cancelled at end of main.
	phaser := phase.FromContext(context.Background())

	p0 := phaser.Next()
	go func(p *phase.Phaser) {
		fmt.Println("started p0 func")

		p1 := p.Next()

		type ctxStr string
		// Run some other goroutines which take an ordinary context.
		for i := 0; i < 5; i++ {
			// Phasers can be used like any other context. Let's set a value.
			ctx := context.WithValue(p1, ctxStr("goroutine"), i)
			go func(ctx context.Context) {
				num := ctx.Value("goroutine").(int)
				fmt.Printf("goroutine(%d) started\n", num)
				<-ctx.Done()
				fmt.Printf("goroutine(%d) finished\n", num)
			}(ctx)
		}

		// Wait for cancellation to perform cleanup.
		fmt.Println("Waiting for cancellation")
		<-p1.Done()

		// There is no guarantee on order of completion for the goroutines
		// as they only deal in contexts not Phasers. I can manage this in
		// some other way.
		fmt.Println("Waiting a few seconds for goroutines to finish")
		time.Sleep(3 * time.Second)
		// I have to signal cancellation on the new Phaser I created as well
		// as the one given to me.
		p1.Cancel()
		<-p1.Done()
		p0.Cancel()
	}(p0)

	fmt.Println("Shutdown in 5 seconds")
	time.Sleep(5 * time.Second)
	phaser.Cancel()
	<-phaser.Done() // Wait until everything has finished.
	fmt.Println("Bye!")
}

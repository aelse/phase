package main

// If my application consists of a web server with an internal pipeline
// backed by a database my data flow may look something like:
//
// [ Web API Request ] -> [ Transform Data ] -> [ Persist to DB ]
//
// When I shutdown this system I don't want to lose any data submitted by users.
// What I would like to happen is:
//
// 1. Stop the web server accepting requests
// 2. Empty the queue of any outstanding payloads to transform
// 3. Complete reading the queued, transformed data and persist to DB
// 4. Terminate program

import (
	"context"
	"fmt"
	"time"

	"github.com/aelse/phase"
)

func main() {
	// Create a top level context which will be cancelled at end of main.
	ctx, cancel := phase.Next(context.Background())

	p1, c1 := phase.Next(ctx)
	go component(p1, c1, "db")

	p2, c2 := phase.Next(p1)
	go component(p2, c2, "data pipeline")

	p3, c3 := phase.Next(p2)
	go component(p3, c3, "web server")

	fmt.Println("Shutdown in 5 seconds")
	time.Sleep(5 * time.Second)

	// Cancel this context, which cascades down.
	cancel()
	// Wait until everything has finished.
	<-ctx.Done()
	fmt.Println("Bye!")
}

// simulate any component that needs to perform cleanup at end of context.
func component(ctx context.Context, cancel context.CancelFunc, name string) {
	defer cancel()
	fmt.Printf("%s started\n", name)
	<-ctx.Done()
	fmt.Printf("%s shutting down\n", name)
	time.Sleep(time.Second)
}

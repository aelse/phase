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
	// Create a top lever phaser which will be cancelled at end of main.
	phaser := phase.FromContext(context.Background())

	p0 := phaser.Next()
	// Start the web server
	go component(p0, "web server")

	p1 := p0.Next()
	go component(p1, "data pipeline")

	p2 := p1.Next()
	go component(p2, "db")

	fmt.Println("Shutdown in 5 seconds")
	time.Sleep(5 * time.Second)
	phaser.Cancel()
	<-phaser.Done() // Wait until everything has finished.
	fmt.Println("Bye!")
}

// simulate any component that needs to perform cleanup at end of context.
func component(p *phase.Phaser, name string) {
	defer p.Cancel()
	fmt.Printf("%s started\n", name)
	<-p.Done()
	fmt.Printf("%s shutting down\n", name)
	time.Sleep(time.Second)
}

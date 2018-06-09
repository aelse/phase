# Phase

Phase provides features to enable systems to perform graceful ordered shutdown.

You can use Phase to help you build systems that shutdown in an ordered sequence,
allowng you to drain connections, flush outstanding queue items and persist
data before termination.

## Use

Install the package with `go get github.com/aelse/phase`

A `phase.Phaser` is created from any context object, using `phase.FromContext(ctx)`.

The returned Phaser can be used to create additional child Phasers by calling its `Next()` function, which can also be used to create new Phasers.

* When a Phaser is cancelled its children will terminate in reverse order.
* A phaser may be cancelled by calling `Cancel()` on it to trigger termination of itself and all downstream Phasers.
* Every phaser must have `Cancel()` called on it once its context has terminated.
* A Phaser may be passed to a function expecting a `context.Context` but ordering of shutdown is not guaranteed since that function has no way of signalling the parent when it has completed.

Here's an example implementing a solution to the problem scenario further below.

```
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
func component(p phase.Phaser, name string) {
	defer p.Cancel()
	fmt.Printf("%s started\n", name)
	<-p.Done()
	fmt.Printf("%s shutting down\n", name)
	time.Sleep(time.Second)
}
```

See the [examples](./examples/) directory for working examples.


## Background

Since contexts were released in go 1.7 they have become a staple of many projects.
One common use of contexts is to manage graceful program termination. Unfortunately
contexts on their own are not well suited to this as they do not provide very
granular control over program execution.

Phase solves this problem by providing a Context that handles ordered cancellation.
This allows control of shutdown sequence for Phaser-aware functions, and approximate control of shutdown sequence where functions are only context-aware.

## Problem scenario

For example, if my application consists of a web server with an internal pipeline
backed by a database my data flow may look something like:

[ Web API Request ] -> [ Transform Data ] -> [ Persist to DB ]

When I shutdown this system I don't want to lose any data submitted by users, so
what I would like to happen is

1. Stop the web server accepting requests
2. Empty the queue of any outstanding payloads to transform
3. Complete reading the queued, transformed data and persist to DB
4. Terminate program

When you cancel a context that immediately progagates downstream to any contexts
created from it.
This means you cannot simply cancel a single context at top level because then
you can not choose the order in which your goroutines will terminate.

To have ordered shutdown you have to deal with dependent contexts yourself.
This makes your code busy, prone to error and difficult to troubleshoot.

## Alternatives

Here are similar projects I'm aware of.

* [ash2k/stager](https://github.com/ash2k/stager)
* [icholy/killable](https://github.com/icholy/killable)
* [tomb](https://gopkg.in/tomb.v1)
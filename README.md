![Tests](https://github.com/aelse/phase/actions/workflows/tests.yml/badge.svg)
![Lint](https://github.com/aelse/phase/actions/workflows/golangci-lint.yml/badge.svg)
[![codecov](https://codecov.io/gh/aelse/phase/branch/master/graph/badge.svg)](https://codecov.io/gh/aelse/phase)
[![License](https://img.shields.io/github/license/aelse/phase.svg)](https://github.com/aelse/phase/blob/master/LICENSE)
[![GoDoc](https://godoc.org/github.com/aelse/phase?status.svg)](https://godoc.org/github.com/aelse/phase)

# Phase

**Phase** helps Go applications perform _graceful, ordered shutdown_ — giving you explicit control over how subsystems wind down.

Use Phase to ensure resources are released in the right order: servers stop accepting connections before pipelines drain, and pipelines complete before writing to the database ends.

---

## 🚦 Why Phase?

Imagine your app has this data flow:

[ Web API Request ] -> [ Transform Data ] -> [ Persist to DB ]

During shutdown, you want:

1. The web server to stop accepting new requests
2. Outstanding work in the transformation queue to finish
3. Final transformed data to be persisted to the database
4. The program to exit cleanly

But standard `context.Context` cancels everything _immediately_ downstream, making it hard to enforce this order. You’d need to manually coordinate dependent goroutines — a recipe for bugs and complexity.

**Phase** solves this by chaining context lifecycles with parent-child awareness. Shutdown proceeds in reverse order of creation, giving you fine-grained control with a simple API.

---

## 🧠 Background

Go’s `context.Context` is often used to manage cancellation. However, it wasn’t designed for ordered shutdown of interdependent goroutines.

Phase extends this concept by introducing a `Phaser` — a context with awareness of its children. It gives you a simple interface to create, cancel, wait, and close contexts in a reliable, deterministic order.

---

## ✨ Features

- ✅ Works anywhere a `context.Context` is accepted
- 📚 Automatically tracks child phases
- ⛓ Graceful teardown in reverse order of creation
- 🧹 Guarantees that cleanup completes before parent exits

---

## 🚀 Getting Started

Install:

```bash
go get github.com/aelse/phase
```

Create a root Phaser from any context:

```go
phaser := phase.FromContext(context.Background())
```

Spawn child phases using Next():

```go
child := phaser.Next()
```

Cancel and wait for shutdown in reverse order:

```go
child.Cancel()
phase.Wait(child)
phase.Close(child)
```

## 🔧 Example

```go
package main

import (
	"context"
	"fmt"
	"time"

	"github.com/aelse/phase"
)

func main() {
	// Create root phaser
	ph := phase.FromContext(context.Background())

	// Each subsystem is a child of the previous
	web := ph.Next()
	go component(web, "Web Server")

	pipeline := web.Next()
	go component(pipeline, "Data Pipeline")

	db := pipeline.Next()
	go component(db, "Database")

	fmt.Println("Shutting down in 5 seconds...")
	time.Sleep(5 * time.Second)

	// Start shutdown from root
	ph.Cancel()
	<-ph.Done() // Wait for all phases to complete
	fmt.Println("All systems shut down.")
}

func component(p *phase.Phaser, name string) {
	defer p.Cancel()
	fmt.Printf("[%s] started\n", name)
	<-p.Done()
	fmt.Printf("[%s] shutting down\n", name)
	time.Sleep(time.Second)
}
```

🔎 See the examples directory for more complete use cases.

## 🔁 Alternatives

Projects with similar goals:

* ash2k/stager
* skovtunenko/graterm
* icholy/killable
* tomb

## 🪪 License

This project is licensed under the MIT License. See LICENSE for details.

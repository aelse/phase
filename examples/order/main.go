package main

import (
	"context"
	"fmt"

	"github.com/aelse/phase"
)

func main() {
	var phaser, p0, p1, p2 *phase.Phaser
	phaser = phase.FromContext(context.Background())
	p0 = phaser.Next()
	p1 = p0.Next()
	p2 = p1.Next()

	phaser.Cancel() // Cancel Phaser chain

	// We expect contexts to end in order: p2, p1, p0, phaser.

	select {
	case <-phaser.Done():
		fmt.Println("phaser ended early")
	case <-p0.Done():
		fmt.Println("p0 ended early")
	case <-p1.Done():
		fmt.Println("p1 ended early")
	case <-p2.Done():
		fmt.Println("p2 ended in order")
		p2.Cancel()
	}

	select {
	case <-phaser.Done():
		fmt.Println("phaser ended early")
	case <-p0.Done():
		fmt.Println("p0 ended early")
	case <-p1.Done():
		fmt.Println("p1 ended in order")
		p1.Cancel()
	}

	select {
	case <-phaser.Done():
		fmt.Println("phaser ended early")
	case <-p0.Done():
		fmt.Println("p0 ended in order")
		p0.Cancel()
	}

	<-phaser.Done()
	fmt.Println("finished!")
}

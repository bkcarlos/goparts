package main

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/bkcarlos/goparts/lifecycle"
)

func main() {
	m, err := lifecycle.New(lifecycle.Config{ShutdownTimeout: time.Second})
	if err != nil {
		log.Fatal(err)
	}
	if err := m.Add("worker", func(ctx context.Context) error {
		fmt.Println("worker started")
		<-ctx.Done()
		fmt.Println("worker stopped")
		return ctx.Err()
	}); err != nil {
		log.Fatal(err)
	}
	if err := m.OnStop("resources", func(ctx context.Context) error {
		fmt.Println("resources released")
		return nil
	}); err != nil {
		log.Fatal(err)
	}
	// The demo exits automatically; a real service can use context.Background().
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	if err := m.RunSignals(ctx); err != nil {
		log.Fatal(err)
	}
}

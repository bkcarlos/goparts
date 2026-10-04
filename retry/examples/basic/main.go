package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"time"

	"github.com/bkcarlos/goparts/retry"
)

func main() {
	transient := errors.New("temporary failure")
	r, err := retry.New(retry.Config{
		MaxAttempts:  3,
		InitialDelay: 10 * time.Millisecond,
		Jitter:       0.2,
		RetryIf:      func(err error) bool { return errors.Is(err, transient) },
	})
	if err != nil {
		log.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	attempts := 0
	if err := r.Do(ctx, func(ctx context.Context) error {
		attempts++
		if attempts < 3 {
			return transient
		}
		return nil
	}); err != nil {
		log.Fatal(err)
	}
	fmt.Printf("succeeded after %d attempts\n", attempts)
}

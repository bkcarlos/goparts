package retry

import (
	"math/rand"
	"sync"
	"testing"
	"time"
)

func TestDeterministicConcurrentRandom(t *testing.T) {
	a, _ := New(Config{Jitter: 0.5, RandomSource: rand.NewSource(42)})
	b, _ := New(Config{Jitter: 0.5, RandomSource: rand.NewSource(42)})
	for i := 0; i < 10; i++ {
		if a.jitter(time.Second) != b.jitter(time.Second) {
			t.Fatal("nondeterministic")
		}
	}
	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); a.jitter(time.Second) }()
	}
	wg.Wait()
}

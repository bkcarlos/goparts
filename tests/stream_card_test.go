package integration_test

import (
	"context"
	"github.com/bkcarlos/goparts/llm/chat"
	"github.com/bkcarlos/goparts/workerpool"
	"sync"
	"testing"
	"time"
)

func TestAccumulatedCardUpdatesKeepSequence(t *testing.T) {
	ctx := context.Background()
	stream, err := workerpool.NewStream[string](2, 10)
	if err != nil {
		t.Fatal(err)
	}
	var sequence chat.SequenceAllocator[string]
	var mu sync.Mutex
	var updates []string
	var numbers []uint64
	a, err := chat.NewAccumulator(ctx, chat.AccumulatorConfig{Characters: 1, Interval: time.Second, Flush: func(ctx context.Context, text string) error {
		return stream.Submit(ctx, "card", func(context.Context) error {
			n := sequence.Next("card")
			mu.Lock()
			updates = append(updates, text)
			numbers = append(numbers, n)
			mu.Unlock()
			return nil
		})
	}})
	if err != nil {
		t.Fatal(err)
	}
	for _, part := range []string{"你", "好", "！"} {
		if err = a.Add(part); err != nil {
			t.Fatal(err)
		}
	}
	if err = a.Close(); err != nil {
		t.Fatal(err)
	}
	stream.Close()
	if err = stream.Wait(ctx); err != nil {
		t.Fatal(err)
	}
	if len(updates) != 3 || updates[2] != "你好！" || numbers[0] != 1 || numbers[2] != 3 {
		t.Fatal(updates, numbers)
	}
}

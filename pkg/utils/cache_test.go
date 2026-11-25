package utils

import (
	"context"
	"errors"
	"slices"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestResourceCache_Get(t *testing.T) {
	ctx := context.Background()
	want := []string{"item1", "item2"}

	t.Run("fetches on first call", func(t *testing.T) {
		cache := &ResourceCache[string]{}
		callCount := 0
		fetch := func(context.Context) ([]string, error) {
			callCount++
			return want, nil
		}

		items, err := cache.Get(ctx, time.Minute, "test resource", fetch)
		if err != nil {
			t.Fatalf("Get: %v", err)
		}
		if !slices.Equal(items, want) || callCount != 1 {
			t.Errorf("got %v after %d fetches, want %v after 1", items, callCount, want)
		}
	})

	t.Run("uses cache when within TTL", func(t *testing.T) {
		cache := &ResourceCache[string]{}
		callCount := 0
		fetch := func(context.Context) ([]string, error) {
			callCount++
			return want, nil
		}

		for range 2 {
			items, err := cache.Get(ctx, time.Minute, "test resource", fetch)
			if err != nil {
				t.Fatalf("Get: %v", err)
			}
			if !slices.Equal(items, want) {
				t.Errorf("got %v, want %v", items, want)
			}
		}
		if callCount != 1 {
			t.Errorf("fetched %d times, want 1", callCount)
		}
	})

	t.Run("refetches when TTL expired", func(t *testing.T) {
		cache := &ResourceCache[string]{}
		callCount := 0
		fetch := func(context.Context) ([]string, error) {
			callCount++
			return want, nil
		}

		// A zero TTL makes every entry already expired, so no sleep is needed.
		for range 2 {
			if _, err := cache.Get(ctx, 0, "test resource", fetch); err != nil {
				t.Fatalf("Get: %v", err)
			}
		}
		if callCount != 2 {
			t.Errorf("fetched %d times, want 2", callCount)
		}
	})

	t.Run("propagates errors from fetch function", func(t *testing.T) {
		cache := &ResourceCache[string]{}
		wantErr := errors.New("fetch error")

		items, err := cache.Get(ctx, time.Minute, "test resource", func(context.Context) ([]string, error) {
			return nil, wantErr
		})
		if !errors.Is(err, wantErr) || items != nil {
			t.Errorf("got (%v, %v), want (nil, %v)", items, err, wantErr)
		}
	})

	t.Run("concurrent callers share one fetch", func(t *testing.T) {
		cache := &ResourceCache[int]{}
		var callCount atomic.Int32
		started := make(chan struct{})
		release := make(chan struct{})
		fetch := func(context.Context) ([]int, error) {
			if callCount.Add(1) == 1 {
				close(started)
			}
			<-release
			return []int{1, 2, 3}, nil
		}

		var wg sync.WaitGroup
		for range 10 {
			wg.Go(func() {
				items, err := cache.Get(ctx, time.Minute, "test resource", fetch)
				if err != nil || !slices.Equal(items, []int{1, 2, 3}) {
					t.Errorf("got (%v, %v), want ([1 2 3], nil)", items, err)
				}
			})
		}
		// Hold the first fetch open: every other caller either waits on the lock
		// behind it or arrives after it and hits the cache.
		<-started
		close(release)
		wg.Wait()

		if n := callCount.Load(); n != 1 {
			t.Errorf("fetched %d times, want 1", n)
		}
	})
}

func TestResourceCache_GetFiltered(t *testing.T) {
	ctx := context.Background()
	cache := &ResourceCache[int]{}
	callCount := 0
	fetch := func(context.Context) ([]int, error) {
		callCount++
		return []int{1, 2, 3, 4, 5}, nil
	}

	for _, tc := range []struct {
		name   string
		filter func(int) bool
		want   []int
	}{
		{"even", func(i int) bool { return i%2 == 0 }, []int{2, 4}},
		{"less than 3, served from cache", func(i int) bool { return i < 3 }, []int{1, 2}},
	} {
		items, err := cache.GetFiltered(ctx, time.Minute, "test numbers", fetch, tc.filter)
		if err != nil {
			t.Fatalf("%s: %v", tc.name, err)
		}
		if !slices.Equal(items, tc.want) {
			t.Errorf("%s: got %v, want %v", tc.name, items, tc.want)
		}
	}
	if callCount != 1 {
		t.Errorf("fetched %d times, want 1", callCount)
	}
}

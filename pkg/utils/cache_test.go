package utils

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestResourceCache_Get(t *testing.T) {
	t.Run("fetches on first call", func(t *testing.T) {
		cache := &ResourceCache[string]{}
		ctx := context.Background()
		callCount := 0

		fetchFunc := func(ctx context.Context) ([]string, error) {
			callCount++
			return []string{"item1", "item2"}, nil
		}

		items, err := cache.Get(ctx, time.Minute, "test resource", fetchFunc)
		require.NoError(t, err)
		assert.Equal(t, []string{"item1", "item2"}, items)
		assert.Equal(t, 1, callCount)
	})

	t.Run("uses cache when within TTL", func(t *testing.T) {
		cache := &ResourceCache[string]{}
		ctx := context.Background()
		callCount := 0

		fetchFunc := func(ctx context.Context) ([]string, error) {
			callCount++
			return []string{"item1", "item2"}, nil
		}

		_, err := cache.Get(ctx, time.Minute, "test resource", fetchFunc)
		require.NoError(t, err)

		items, err := cache.Get(ctx, time.Minute, "test resource", fetchFunc)
		require.NoError(t, err)
		assert.Equal(t, []string{"item1", "item2"}, items)
		assert.Equal(t, 1, callCount, "fetch function should only be called once")
	})

	t.Run("refetches when TTL expired", func(t *testing.T) {
		cache := &ResourceCache[string]{}
		ctx := context.Background()
		callCount := 0

		fetchFunc := func(ctx context.Context) ([]string, error) {
			callCount++
			return []string{"item1", "item2"}, nil
		}

		_, err := cache.Get(ctx, 10*time.Millisecond, "test resource", fetchFunc)
		require.NoError(t, err)

		time.Sleep(20 * time.Millisecond)

		items, err := cache.Get(ctx, 10*time.Millisecond, "test resource", fetchFunc)
		require.NoError(t, err)
		assert.Equal(t, []string{"item1", "item2"}, items)
		assert.Equal(t, 2, callCount, "fetch function should be called twice")
	})

	t.Run("propagates errors from fetch function", func(t *testing.T) {
		cache := &ResourceCache[string]{}
		ctx := context.Background()
		expectedErr := errors.New("fetch error")

		fetchFunc := func(ctx context.Context) ([]string, error) {
			return nil, expectedErr
		}

		items, err := cache.Get(ctx, time.Minute, "test resource", fetchFunc)
		assert.Equal(t, expectedErr, err)
		assert.Nil(t, items)
	})

	t.Run("handles concurrent access", func(t *testing.T) {
		cache := &ResourceCache[int]{}
		ctx := context.Background()
		var callCount atomic.Int32

		fetchFunc := func(ctx context.Context) ([]int, error) {
			callCount.Add(1)
			time.Sleep(50 * time.Millisecond)
			return []int{1, 2, 3}, nil
		}

		var wg sync.WaitGroup
		goroutines := 10

		for i := 0; i < goroutines; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				items, err := cache.Get(ctx, time.Minute, "test resource", fetchFunc)
				require.NoError(t, err)
				assert.Equal(t, []int{1, 2, 3}, items)
			}()
		}

		wg.Wait()

		assert.Equal(t, int32(1), callCount.Load(), "fetch function should only be called once despite concurrent access")
	})

}

func TestResourceCache_GetFiltered(t *testing.T) {
	t.Run("filters items correctly", func(t *testing.T) {
		cache := &ResourceCache[int]{}
		ctx := context.Background()

		fetchFunc := func(ctx context.Context) ([]int, error) {
			return []int{1, 2, 3, 4, 5}, nil
		}

		filterFunc := func(item int) bool {
			return item%2 == 0
		}

		items, err := cache.GetFiltered(ctx, time.Minute, "test numbers", fetchFunc, filterFunc)
		require.NoError(t, err)
		assert.Equal(t, []int{2, 4}, items)
	})

	t.Run("uses cache when within TTL", func(t *testing.T) {
		cache := &ResourceCache[int]{}
		ctx := context.Background()
		callCount := 0

		fetchFunc := func(ctx context.Context) ([]int, error) {
			callCount++
			return []int{1, 2, 3, 4, 5}, nil
		}

		filterFunc := func(item int) bool {
			return item > 2
		}

		items, err := cache.GetFiltered(ctx, time.Minute, "test numbers", fetchFunc, filterFunc)
		require.NoError(t, err)
		assert.Equal(t, []int{3, 4, 5}, items)
		assert.Equal(t, 1, callCount)

		filterFunc2 := func(item int) bool {
			return item < 3
		}

		items, err = cache.GetFiltered(ctx, time.Minute, "test numbers", fetchFunc, filterFunc2)
		require.NoError(t, err)
		assert.Equal(t, []int{1, 2}, items)
		assert.Equal(t, 1, callCount, "fetch function should only be called once")
	})
}

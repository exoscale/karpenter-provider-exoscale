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

		// First call
		_, err := cache.Get(ctx, time.Minute, "test resource", fetchFunc)
		require.NoError(t, err)

		// Second call within TTL
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

		// First call
		_, err := cache.Get(ctx, 10*time.Millisecond, "test resource", fetchFunc)
		require.NoError(t, err)

		// Wait for TTL to expire
		time.Sleep(20 * time.Millisecond)

		// Second call after TTL
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
			time.Sleep(50 * time.Millisecond) // Simulate slow fetch
			return []int{1, 2, 3}, nil
		}

		var wg sync.WaitGroup
		goroutines := 10

		// Launch multiple goroutines trying to access cache simultaneously
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

		// Due to double-check locking, fetch should be called only once
		assert.Equal(t, int32(1), callCount.Load(), "fetch function should only be called once despite concurrent access")
	})

	t.Run("works with different types", func(t *testing.T) {
		type TestStruct struct {
			ID   int
			Name string
		}

		cache := &ResourceCache[TestStruct]{}
		ctx := context.Background()

		fetchFunc := func(ctx context.Context) ([]TestStruct, error) {
			return []TestStruct{
				{ID: 1, Name: "test1"},
				{ID: 2, Name: "test2"},
			}, nil
		}

		items, err := cache.Get(ctx, time.Minute, "test structs", fetchFunc)
		require.NoError(t, err)
		assert.Len(t, items, 2)
		assert.Equal(t, "test1", items[0].Name)
		assert.Equal(t, "test2", items[1].Name)
	})
}

func TestResourceCache_Invalidate(t *testing.T) {
	t.Run("clears cache and forces refetch", func(t *testing.T) {
		cache := &ResourceCache[string]{}
		ctx := context.Background()
		callCount := 0

		fetchFunc := func(ctx context.Context) ([]string, error) {
			callCount++
			if callCount == 1 {
				return []string{"first"}, nil
			}
			return []string{"second"}, nil
		}

		// First fetch
		items, err := cache.Get(ctx, time.Minute, "test resource", fetchFunc)
		require.NoError(t, err)
		assert.Equal(t, []string{"first"}, items)

		// Invalidate
		cache.Invalidate()

		// Should fetch again
		items, err = cache.Get(ctx, time.Minute, "test resource", fetchFunc)
		require.NoError(t, err)
		assert.Equal(t, []string{"second"}, items)
		assert.Equal(t, 2, callCount)
	})

	t.Run("is thread-safe", func(t *testing.T) {
		cache := &ResourceCache[int]{}
		ctx := context.Background()
		var callCount atomic.Int32

		fetchFunc := func(ctx context.Context) ([]int, error) {
			callCount.Add(1)
			return []int{int(callCount.Load())}, nil
		}

		var wg sync.WaitGroup

		// Concurrent Get and Invalidate operations
		for i := 0; i < 10; i++ {
			wg.Add(2)
			go func() {
				defer wg.Done()
				_, _ = cache.Get(ctx, time.Millisecond, "test", fetchFunc)
			}()
			go func() {
				defer wg.Done()
				cache.Invalidate()
			}()
		}

		wg.Wait()
		// Test passes if no race condition detected
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
			return item%2 == 0 // Keep only even numbers
		}

		items, err := cache.GetFiltered(ctx, time.Minute, "test numbers", fetchFunc, filterFunc)
		require.NoError(t, err)
		assert.Equal(t, []int{2, 4}, items)
	})

	t.Run("returns empty slice when no items match", func(t *testing.T) {
		cache := &ResourceCache[string]{}
		ctx := context.Background()

		fetchFunc := func(ctx context.Context) ([]string, error) {
			return []string{"apple", "banana", "cherry"}, nil
		}

		filterFunc := func(item string) bool {
			return item == "orange" // No match
		}

		items, err := cache.GetFiltered(ctx, time.Minute, "test fruits", fetchFunc, filterFunc)
		require.NoError(t, err)
		assert.Empty(t, items)
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

		// First call
		items, err := cache.GetFiltered(ctx, time.Minute, "test numbers", fetchFunc, filterFunc)
		require.NoError(t, err)
		assert.Equal(t, []int{3, 4, 5}, items)
		assert.Equal(t, 1, callCount)

		// Second call within TTL with different filter
		filterFunc2 := func(item int) bool {
			return item < 3
		}

		items, err = cache.GetFiltered(ctx, time.Minute, "test numbers", fetchFunc, filterFunc2)
		require.NoError(t, err)
		assert.Equal(t, []int{1, 2}, items)
		assert.Equal(t, 1, callCount, "fetch function should only be called once")
	})

	t.Run("propagates errors from fetch function", func(t *testing.T) {
		cache := &ResourceCache[string]{}
		ctx := context.Background()
		expectedErr := errors.New("fetch error")

		fetchFunc := func(ctx context.Context) ([]string, error) {
			return nil, expectedErr
		}

		filterFunc := func(item string) bool {
			return true
		}

		items, err := cache.GetFiltered(ctx, time.Minute, "test resource", fetchFunc, filterFunc)
		assert.Equal(t, expectedErr, err)
		assert.Nil(t, items)
	})

	t.Run("works with struct types and field matching", func(t *testing.T) {
		type Person struct {
			Name string
			Age  int
		}

		cache := &ResourceCache[Person]{}
		ctx := context.Background()

		fetchFunc := func(ctx context.Context) ([]Person, error) {
			return []Person{
				{Name: "Alice", Age: 30},
				{Name: "Bob", Age: 25},
				{Name: "Charlie", Age: 35},
			}, nil
		}

		// Filter by name
		filterFunc := func(p Person) bool {
			return p.Name == "Bob"
		}

		items, err := cache.GetFiltered(ctx, time.Minute, "test persons", fetchFunc, filterFunc)
		require.NoError(t, err)
		assert.Len(t, items, 1)
		assert.Equal(t, "Bob", items[0].Name)
		assert.Equal(t, 25, items[0].Age)
	})

	t.Run("handles concurrent filtered access", func(t *testing.T) {
		cache := &ResourceCache[int]{}
		ctx := context.Background()
		var callCount atomic.Int32

		fetchFunc := func(ctx context.Context) ([]int, error) {
			callCount.Add(1)
			time.Sleep(50 * time.Millisecond) // Simulate slow fetch
			return []int{1, 2, 3, 4, 5, 6, 7, 8, 9, 10}, nil
		}

		var wg sync.WaitGroup
		goroutines := 10

		// Launch multiple goroutines with different filters
		for i := 0; i < goroutines; i++ {
			wg.Add(1)
			threshold := i
			go func() {
				defer wg.Done()
				filterFunc := func(item int) bool {
					return item > threshold
				}
				items, err := cache.GetFiltered(ctx, time.Minute, "test numbers", fetchFunc, filterFunc)
				require.NoError(t, err)
				assert.NotEmpty(t, items)
			}()
		}

		wg.Wait()

		// Due to double-check locking, fetch should be called only once
		assert.Equal(t, int32(1), callCount.Load(), "fetch function should only be called once despite concurrent access")
	})

	t.Run("returns all items when filter always returns true", func(t *testing.T) {
		cache := &ResourceCache[string]{}
		ctx := context.Background()

		fetchFunc := func(ctx context.Context) ([]string, error) {
			return []string{"a", "b", "c"}, nil
		}

		filterFunc := func(item string) bool {
			return true
		}

		items, err := cache.GetFiltered(ctx, time.Minute, "test items", fetchFunc, filterFunc)
		require.NoError(t, err)
		assert.Equal(t, []string{"a", "b", "c"}, items)
	})
}

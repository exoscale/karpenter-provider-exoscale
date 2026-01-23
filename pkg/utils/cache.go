package utils

import (
	"context"
	"sync"
	"time"

	"sigs.k8s.io/controller-runtime/pkg/log"
)

// ResourceCache provides a thread-safe cache for resource lists with TTL support.
// It uses double-check locking pattern to avoid redundant API calls during concurrent access.
type ResourceCache[T any] struct {
	items     []T
	timestamp time.Time
	mu        sync.RWMutex
}

// Get retrieves items from the cache or fetches them using the provided function if cache is stale.
// Parameters:
//   - ctx: context for logging and cancellation
//   - ttl: time-to-live for cached items
//   - resourceName: name of the resource for logging purposes
//   - fetchFunc: function to call when cache needs to be refreshed
func (c *ResourceCache[T]) Get(ctx context.Context, ttl time.Duration, resourceName string, fetchFunc func(context.Context) ([]T, error)) ([]T, error) {
	c.mu.RLock()
	if time.Now().Before(c.timestamp.Add(ttl)) && c.items != nil {
		items := c.items
		defer c.mu.RUnlock()
		log.FromContext(ctx).V(5).Info("using cached "+resourceName, "age", time.Since(c.timestamp))
		return items, nil
	}
	c.mu.RUnlock()

	c.mu.Lock()
	defer c.mu.Unlock()

	// Double-check after acquiring write lock
	if time.Now().Before(c.timestamp.Add(ttl)) && c.items != nil {
		log.FromContext(ctx).V(5).Info("using cached "+resourceName+" (double-check)", "age", time.Since(c.timestamp))
		return c.items, nil
	}

	log.FromContext(ctx).V(1).Info("refreshing " + resourceName + " cache")
	items, err := fetchFunc(ctx)
	if err != nil {
		return nil, err
	}

	c.items = items
	c.timestamp = time.Now()

	return c.items, nil
}

// GetFiltered retrieves items from the cache (refreshing if stale) and applies a filter function.
// This is more efficient than Get when you only need a subset of items, as it avoids
// unnecessary allocations and copies.
// Parameters:
//   - ctx: context for logging and cancellation
//   - ttl: time-to-live for cached items
//   - resourceName: name of the resource for logging purposes
//   - fetchFunc: function to call when cache needs to be refreshed
//   - filterFunc: function to filter items from the cache
func (c *ResourceCache[T]) GetFiltered(ctx context.Context, ttl time.Duration, resourceName string, fetchFunc func(context.Context) ([]T, error), filterFunc func(T) bool) ([]T, error) {
	items, err := c.Get(ctx, ttl, resourceName, fetchFunc)
	if err != nil {
		return nil, err
	}

	filtered := make([]T, 0)
	for _, item := range items {
		if filterFunc(item) {
			filtered = append(filtered, item)
		}
	}

	return filtered, nil
}

// Invalidate clears the cache, forcing the next Get call to fetch fresh data.
func (c *ResourceCache[T]) Invalidate() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.items = nil
	c.timestamp = time.Time{}
}

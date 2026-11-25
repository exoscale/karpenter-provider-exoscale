package utils

import (
	"context"
	"sync"
	"time"

	"sigs.k8s.io/controller-runtime/pkg/log"
)

// ResourceCache is a TTL cache for a resource list. Concurrent callers on a
// stale cache share a single fetch.
type ResourceCache[T any] struct {
	items     []T
	timestamp time.Time
	mu        sync.RWMutex
}

func (c *ResourceCache[T]) Get(ctx context.Context, ttl time.Duration, resourceName string, fetchFunc func(context.Context) ([]T, error)) ([]T, error) {
	c.mu.RLock()
	if c.items != nil && time.Since(c.timestamp) < ttl {
		defer c.mu.RUnlock()
		log.FromContext(ctx).V(5).Info("using cached "+resourceName, "age", time.Since(c.timestamp))
		return c.items, nil
	}
	c.mu.RUnlock()

	c.mu.Lock()
	defer c.mu.Unlock()

	// Another caller may have refreshed while we waited for the write lock.
	if c.items != nil && time.Since(c.timestamp) < ttl {
		return c.items, nil
	}

	log.FromContext(ctx).V(1).Info("refreshing " + resourceName + " cache")
	items, err := fetchFunc(ctx)
	if err != nil {
		return nil, err
	}

	c.items = items
	c.timestamp = time.Now()
	return items, nil
}

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

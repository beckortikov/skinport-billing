package skinport

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"golang.org/x/sync/errgroup"
	"golang.org/x/sync/singleflight"
)

const (
	refreshTimeout = 30 * time.Second
	// Skinport allows 8 requests per 5 minutes and every refresh costs two,
	// so after a failure we keep serving stale data instead of retrying on
	// each incoming request.
	retryAfterError = time.Minute
)

type itemsFetcher interface {
	Items(ctx context.Context, tradable bool) ([]MarketItem, error)
}

// Cache keeps merged Skinport items in memory and refreshes them lazily.
type Cache struct {
	fetcher itemsFetcher
	ttl     time.Duration
	now     func() time.Time

	group singleflight.Group

	mu        sync.RWMutex
	items     []Item
	expiresAt time.Time
}

func NewCache(f itemsFetcher, ttl time.Duration) *Cache {
	return &Cache{fetcher: f, ttl: ttl, now: time.Now}
}

// Items returns cached items, fetching them from Skinport when the cache is
// empty or expired. The returned slice is shared and must not be modified.
func (c *Cache) Items(ctx context.Context) ([]Item, error) {
	c.mu.RLock()
	items, expiresAt := c.items, c.expiresAt
	c.mu.RUnlock()

	if items != nil && c.now().Before(expiresAt) {
		return items, nil
	}

	// The refresh runs detached from the caller's context: one client
	// disconnecting must not fail the fetch for everyone waiting on it.
	ch := c.group.DoChan("items", func() (any, error) {
		return c.refresh()
	})

	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case res := <-ch:
		if res.Err != nil {
			return nil, res.Err
		}
		return res.Val.([]Item), nil
	}
}

func (c *Cache) refresh() ([]Item, error) {
	ctx, cancel := context.WithTimeout(context.Background(), refreshTimeout)
	defer cancel()

	items, err := c.fetch(ctx)
	now := c.now()

	c.mu.Lock()
	defer c.mu.Unlock()

	if err != nil {
		if c.items == nil {
			return nil, err
		}
		slog.Warn("skinport refresh failed, serving stale items", "error", err)
		c.expiresAt = now.Add(retryAfterError)
		return c.items, nil
	}

	c.items = items
	c.expiresAt = now.Add(c.ttl)
	return items, nil
}

func (c *Cache) fetch(ctx context.Context) ([]Item, error) {
	var tradable, nonTradable []MarketItem

	g, ctx := errgroup.WithContext(ctx)
	g.Go(func() error {
		var err error
		tradable, err = c.fetcher.Items(ctx, true)
		return err
	})
	g.Go(func() error {
		var err error
		nonTradable, err = c.fetcher.Items(ctx, false)
		return err
	})
	if err := g.Wait(); err != nil {
		return nil, fmt.Errorf("failed to fetch skinport items: %w", err)
	}

	return mergeItems(tradable, nonTradable), nil
}

package skinport

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

type fakeFetcher struct {
	calls atomic.Int32
	err   atomic.Pointer[error]
	delay time.Duration
}

func (f *fakeFetcher) Items(ctx context.Context, tradable bool) ([]MarketItem, error) {
	f.calls.Add(1)
	time.Sleep(f.delay)
	if err := f.err.Load(); err != nil {
		return nil, *err
	}
	if tradable {
		return []MarketItem{
			{MarketHashName: "B", MinPrice: ptr(2.0)},
			{MarketHashName: "Knife", Version: ptr("Phase 1"), MinPrice: ptr(100.0)},
		}, nil
	}
	return []MarketItem{
		{MarketHashName: "B", MinPrice: ptr(1.5)},
		{MarketHashName: "A", MinPrice: nil},
		{MarketHashName: "Knife", Version: ptr("Phase 2"), MinPrice: ptr(90.0)},
	}, nil
}

func ptr[T any](v T) *T { return &v }

func TestCacheMergesListings(t *testing.T) {
	c := NewCache(&fakeFetcher{}, time.Minute)

	items, err := c.Items(context.Background())
	if err != nil {
		t.Fatal(err)
	}

	want := []struct {
		name, version         string
		tradable, nonTradable *float64
	}{
		{"A", "", nil, nil},
		{"B", "", ptr(2.0), ptr(1.5)},
		{"Knife", "Phase 1", ptr(100.0), nil},
		{"Knife", "Phase 2", nil, ptr(90.0)},
	}
	if len(items) != len(want) {
		t.Fatalf("got %d items, want %d: %+v", len(items), len(want), items)
	}
	for i, w := range want {
		it := items[i]
		if it.MarketHashName != w.name || deref(it.Version) != w.version ||
			!equalPrice(it.MinPriceTradable, w.tradable) || !equalPrice(it.MinPriceNonTradable, w.nonTradable) {
			t.Errorf("item %d = %+v, want %+v", i, it, w)
		}
	}
}

func equalPrice(a, b *float64) bool {
	if a == nil || b == nil {
		return a == b
	}
	return *a == *b
}

func TestCacheDeduplicatesConcurrentRefresh(t *testing.T) {
	f := &fakeFetcher{delay: 50 * time.Millisecond}
	c := NewCache(f, time.Minute)

	var wg sync.WaitGroup
	for range 20 {
		wg.Go(func() {
			if _, err := c.Items(context.Background()); err != nil {
				t.Error(err)
			}
		})
	}
	wg.Wait()

	if got := f.calls.Load(); got != 2 {
		t.Fatalf("fetcher called %d times, want 2", got)
	}
}

func TestCacheExpiry(t *testing.T) {
	f := &fakeFetcher{}
	c := NewCache(f, time.Minute)
	now := time.Now()
	c.now = func() time.Time { return now }

	c.Items(context.Background())
	c.Items(context.Background())
	if got := f.calls.Load(); got != 2 {
		t.Fatalf("fetcher called %d times before expiry, want 2", got)
	}

	now = now.Add(time.Minute + time.Second)
	c.Items(context.Background())
	if got := f.calls.Load(); got != 4 {
		t.Fatalf("fetcher called %d times after expiry, want 4", got)
	}
}

func TestCacheServesStaleOnError(t *testing.T) {
	f := &fakeFetcher{}
	c := NewCache(f, time.Minute)
	now := time.Now()
	c.now = func() time.Time { return now }

	fresh, err := c.Items(context.Background())
	if err != nil {
		t.Fatal(err)
	}

	upstreamErr := errors.New("429")
	f.err.Store(&upstreamErr)
	now = now.Add(2 * time.Minute)

	stale, err := c.Items(context.Background())
	if err != nil {
		t.Fatalf("expected stale items, got error: %v", err)
	}
	if len(stale) != len(fresh) {
		t.Fatalf("stale items differ from cached ones")
	}

	calls := f.calls.Load()
	c.Items(context.Background())
	if f.calls.Load() != calls {
		t.Fatal("upstream retried right after a failure")
	}
}

func TestCacheErrorWithoutData(t *testing.T) {
	f := &fakeFetcher{}
	upstreamErr := errors.New("boom")
	f.err.Store(&upstreamErr)

	if _, err := NewCache(f, time.Minute).Items(context.Background()); !errors.Is(err, upstreamErr) {
		t.Fatalf("err = %v, want %v", err, upstreamErr)
	}
}

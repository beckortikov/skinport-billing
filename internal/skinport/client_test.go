package skinport

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/andybalholm/brotli"
)

func TestClientItems(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Accept-Encoding"); got != "br" {
			t.Errorf("Accept-Encoding = %q, want br", got)
		}
		q := r.URL.Query()
		if q.Get("app_id") != "730" || q.Get("currency") != "EUR" || q.Get("tradable") != "1" {
			t.Errorf("unexpected query: %s", r.URL.RawQuery)
		}

		w.Header().Set("Content-Encoding", "br")
		bw := brotli.NewWriter(w)
		defer bw.Close()
		bw.Write([]byte(`[{"market_hash_name":"AK-47 | Redline (Field-Tested)","version":null,"currency":"EUR","min_price":12.5}]`))
	}))
	defer srv.Close()

	items, err := NewClient(srv.URL, srv.Client()).Items(context.Background(), true)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 || items[0].MarketHashName != "AK-47 | Redline (Field-Tested)" || *items[0].MinPrice != 12.5 {
		t.Fatalf("unexpected items: %+v", items)
	}
}

func TestClientItemsErrorStatus(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "rate limited", http.StatusTooManyRequests)
	}))
	defer srv.Close()

	if _, err := NewClient(srv.URL, srv.Client()).Items(context.Background(), false); err == nil {
		t.Fatal("expected error on 429")
	}
}

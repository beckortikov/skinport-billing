package skinport

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"

	"github.com/andybalholm/brotli"
)

const (
	DefaultBaseURL = "https://api.skinport.com/v1"

	defaultAppID    = "730"
	defaultCurrency = "EUR"
)

// MarketItem is a single entry of the Skinport /items response.
type MarketItem struct {
	MarketHashName string   `json:"market_hash_name"`
	Version        *string  `json:"version"`
	Currency       string   `json:"currency"`
	SuggestedPrice *float64 `json:"suggested_price"`
	ItemPage       string   `json:"item_page"`
	MarketPage     string   `json:"market_page"`
	MinPrice       *float64 `json:"min_price"`
}

// Client is a minimal Skinport public API client.
type Client struct {
	baseURL string
	hc      *http.Client
}

func NewClient(baseURL string, hc *http.Client) *Client {
	return &Client{baseURL: baseURL, hc: hc}
}

// Items returns market items for the default app and currency.
func (c *Client) Items(ctx context.Context, tradable bool) ([]MarketItem, error) {
	q := url.Values{}
	q.Set("app_id", defaultAppID)
	q.Set("currency", defaultCurrency)
	q.Set("tradable", "0")
	if tradable {
		q.Set("tradable", "1")
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+"/items?"+q.Encode(), nil)
	if err != nil {
		return nil, fmt.Errorf("failed to build request: %w", err)
	}
	// Skinport rejects /items without brotli. Setting the header manually
	// disables transparent decompression in net/http, so we decode ourselves.
	req.Header.Set("Accept-Encoding", "br")
	req.Header.Set("Accept", "application/json")

	resp, err := c.hc.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch items: %w", err)
	}
	defer resp.Body.Close()

	var body io.Reader = resp.Body
	if resp.Header.Get("Content-Encoding") == "br" {
		body = brotli.NewReader(resp.Body)
	}

	if resp.StatusCode != http.StatusOK {
		msg, _ := io.ReadAll(io.LimitReader(body, 512))
		return nil, fmt.Errorf("skinport responded %d: %s", resp.StatusCode, msg)
	}

	var items []MarketItem
	if err := json.NewDecoder(body).Decode(&items); err != nil {
		return nil, fmt.Errorf("failed to decode items: %w", err)
	}
	return items, nil
}

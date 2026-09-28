package skinport

import (
	"cmp"
	"slices"
)

// Item is a market item with minimal prices for both tradable and
// non-tradable (trade-locked) listings. A nil price means there are no
// listings of that kind.
type Item struct {
	MarketHashName      string   `json:"market_hash_name"`
	Version             *string  `json:"version"`
	Currency            string   `json:"currency"`
	SuggestedPrice      *float64 `json:"suggested_price"`
	ItemPage            string   `json:"item_page"`
	MarketPage          string   `json:"market_page"`
	MinPriceTradable    *float64 `json:"min_price_tradable"`
	MinPriceNonTradable *float64 `json:"min_price_non_tradable"`
}

type itemKey struct {
	name    string
	version string
}

// market_hash_name alone is not unique: Doppler phases and similar share
// the name and differ only by version.
func keyOf(m MarketItem) itemKey {
	k := itemKey{name: m.MarketHashName}
	if m.Version != nil {
		k.version = *m.Version
	}
	return k
}

func newItem(m MarketItem) Item {
	return Item{
		MarketHashName: m.MarketHashName,
		Version:        m.Version,
		Currency:       m.Currency,
		SuggestedPrice: m.SuggestedPrice,
		ItemPage:       m.ItemPage,
		MarketPage:     m.MarketPage,
	}
}

// mergeItems joins both listings by item key. The sets overlap only
// partially, so an item may be present in just one of them.
func mergeItems(tradable, nonTradable []MarketItem) []Item {
	items := make([]Item, 0, len(tradable)+len(nonTradable)/2)
	idx := make(map[itemKey]int, len(tradable))

	for _, m := range tradable {
		it := newItem(m)
		it.MinPriceTradable = m.MinPrice
		idx[keyOf(m)] = len(items)
		items = append(items, it)
	}

	for _, m := range nonTradable {
		if i, ok := idx[keyOf(m)]; ok {
			items[i].MinPriceNonTradable = m.MinPrice
			continue
		}
		it := newItem(m)
		it.MinPriceNonTradable = m.MinPrice
		items = append(items, it)
	}

	slices.SortFunc(items, func(a, b Item) int {
		return cmp.Or(
			cmp.Compare(a.MarketHashName, b.MarketHashName),
			cmp.Compare(deref(a.Version), deref(b.Version)),
		)
	})
	return items
}

func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

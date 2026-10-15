package billing_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/beckortikov/skinport-billing/internal/billing"
	"github.com/beckortikov/skinport-billing/internal/httpapi"
)

// A client on a flaky connection resends the same withdrawal. The retry must
// return the original operation instead of debiting the balance again.
//
// It lives here rather than in httpapi so that all database tests run in one
// package and don't race each other on the shared tables.
func TestWithdrawRetryOverHTTP(t *testing.T) {
	svc, _ := setup(t, 10000)
	h := httpapi.NewHandler(nil, svc)

	withdraw := func() billing.Withdrawal {
		t.Helper()
		req := httptest.NewRequest(http.MethodPost, "/users/1/withdrawals", strings.NewReader(`{"amount_cents":2500}`))
		req.Header.Set("Idempotency-Key", "order-42")
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)

		if rec.Code != http.StatusCreated {
			t.Fatalf("status = %d, want 201; body: %s", rec.Code, rec.Body)
		}
		var w billing.Withdrawal
		if err := json.NewDecoder(rec.Body).Decode(&w); err != nil {
			t.Fatal(err)
		}
		return w
	}

	first := withdraw()
	retry := withdraw()
	if retry.ID != first.ID || retry.BalanceAfter != 7500 {
		t.Fatalf("retry = %+v, want the original withdrawal %+v", retry, first)
	}

	history, err := svc.Withdrawals(t.Context(), 1, 10)
	if err != nil {
		t.Fatal(err)
	}
	u, err := svc.User(t.Context(), 1)
	if err != nil {
		t.Fatal(err)
	}
	if u.Balance != 7500 || len(history) != 1 {
		t.Fatalf("balance = %d, records = %d; want 7500 and 1", u.Balance, len(history))
	}
}

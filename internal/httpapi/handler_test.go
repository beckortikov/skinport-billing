package httpapi

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// These cases are rejected before reaching the billing service, so no
// database is needed.
func TestWithdrawValidation(t *testing.T) {
	h := NewHandler(nil, nil)

	tests := []struct {
		name string
		path string
		body string
	}{
		{"non-numeric id", "/users/abc/withdrawals", `{"amount_cents":100}`},
		{"zero id", "/users/0/withdrawals", `{"amount_cents":100}`},
		{"malformed json", "/users/1/withdrawals", `{"amount_cents":`},
		{"fractional amount", "/users/1/withdrawals", `{"amount_cents":10.5}`},
		{"string amount", "/users/1/withdrawals", `{"amount_cents":"100"}`},
		{"unknown field", "/users/1/withdrawals", `{"amount_cents":100,"user_id":2}`},
		{"amount without unit", "/users/1/withdrawals", `{"amount":100}`},
		{"missing amount", "/users/1/withdrawals", `{}`},
		{"negative amount", "/users/1/withdrawals", `{"amount_cents":-100}`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, tt.path, strings.NewReader(tt.body))
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, req)

			if rec.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, want 400; body: %s", rec.Code, rec.Body)
			}
		})
	}
}

func TestServeSpec(t *testing.T) {
	rec := httptest.NewRecorder()
	NewHandler(nil, nil).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/openapi.yaml", nil))

	if rec.Code != http.StatusOK || !strings.HasPrefix(rec.Body.String(), "openapi: 3") {
		t.Fatalf("status = %d, body starts with %.20q", rec.Code, rec.Body)
	}
}

func TestHistoryLimitValidation(t *testing.T) {
	h := NewHandler(nil, nil)

	for _, limit := range []string{"0", "-1", "abc", "501"} {
		req := httptest.NewRequest(http.MethodGet, "/users/1/withdrawals?limit="+limit, nil)
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)

		if rec.Code != http.StatusBadRequest {
			t.Errorf("limit=%s: status = %d, want 400", limit, rec.Code)
		}
	}
}

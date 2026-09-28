package httpapi

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strconv"

	"github.com/beckortikov/skinport-billing/internal/billing"
	"github.com/beckortikov/skinport-billing/internal/skinport"
)

const (
	defaultHistoryLimit = 50
	maxHistoryLimit     = 500
)

type handler struct {
	items   *skinport.Cache
	billing *billing.Service
}

// NewHandler returns the HTTP API router.
func NewHandler(items *skinport.Cache, b *billing.Service) http.Handler {
	h := &handler{items: items, billing: b}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	mux.HandleFunc("GET /openapi.yaml", serveSpec)
	mux.HandleFunc("GET /docs", serveDocs)
	mux.HandleFunc("GET /items", h.listItems)
	mux.HandleFunc("GET /users/{id}", h.getUser)
	mux.HandleFunc("GET /users/{id}/withdrawals", h.listWithdrawals)
	mux.HandleFunc("POST /users/{id}/withdrawals", h.withdraw)
	return mux
}

func (h *handler) listItems(w http.ResponseWriter, r *http.Request) {
	items, err := h.items.Items(r.Context())
	if err != nil {
		slog.Error("failed to get items", "error", err)
		writeError(w, http.StatusBadGateway, "skinport is unavailable")
		return
	}
	writeJSON(w, http.StatusOK, items)
}

func (h *handler) getUser(w http.ResponseWriter, r *http.Request) {
	userID, ok := userIDFromPath(w, r)
	if !ok {
		return
	}

	u, err := h.billing.User(r.Context(), userID)
	if err != nil {
		writeBillingError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, u)
}

func (h *handler) listWithdrawals(w http.ResponseWriter, r *http.Request) {
	userID, ok := userIDFromPath(w, r)
	if !ok {
		return
	}

	limit := defaultHistoryLimit
	if v := r.URL.Query().Get("limit"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n <= 0 || n > maxHistoryLimit {
			writeError(w, http.StatusBadRequest, "limit must be between 1 and 500")
			return
		}
		limit = n
	}

	history, err := h.billing.Withdrawals(r.Context(), userID, limit)
	if err != nil {
		writeBillingError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, history)
}

type withdrawRequest struct {
	Amount int64 `json:"amount_cents"`
}

func (h *handler) withdraw(w http.ResponseWriter, r *http.Request) {
	userID, ok := userIDFromPath(w, r)
	if !ok {
		return
	}

	var req withdrawRequest
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<10))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.Amount <= 0 {
		writeError(w, http.StatusBadRequest, "amount_cents must be positive")
		return
	}

	wd, err := h.billing.Withdraw(r.Context(), userID, req.Amount)
	if err != nil {
		writeBillingError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, wd)
}

func userIDFromPath(w http.ResponseWriter, r *http.Request) (int64, bool) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id <= 0 {
		writeError(w, http.StatusBadRequest, "invalid user id")
		return 0, false
	}
	return id, true
}

func writeBillingError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, billing.ErrUserNotFound):
		writeError(w, http.StatusNotFound, err.Error())
	case errors.Is(err, billing.ErrInsufficientFunds):
		writeError(w, http.StatusConflict, err.Error())
	case errors.Is(err, billing.ErrInvalidAmount):
		writeError(w, http.StatusBadRequest, err.Error())
	default:
		slog.Error("billing operation failed", "error", err)
		writeError(w, http.StatusInternalServerError, "internal error")
	}
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(v); err != nil {
		slog.Error("failed to write response", "error", err)
	}
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}

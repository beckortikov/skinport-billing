package billing_test

import (
	"context"
	"errors"
	"os"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/beckortikov/skinport-billing/internal/billing"
	"github.com/beckortikov/skinport-billing/internal/postgres"
)

// Tests run against a real database; point TEST_DATABASE_URL at a disposable one.
func setup(t *testing.T, balance int64) (*billing.Service, *pgxpool.Pool) {
	t.Helper()

	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEST_DATABASE_URL is not set")
	}

	ctx := context.Background()
	pool, err := postgres.Connect(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)

	if err := postgres.Migrate(ctx, pool); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `TRUNCATE withdrawals`); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `UPDATE users SET balance = $1 WHERE id = 1`, balance); err != nil {
		t.Fatal(err)
	}
	return billing.NewService(pool), pool
}

func TestWithdraw(t *testing.T) {
	svc, _ := setup(t, 10000)
	ctx := context.Background()

	w, err := svc.Withdraw(ctx, 1, 2500)
	if err != nil {
		t.Fatal(err)
	}
	if w.BalanceBefore != 10000 || w.BalanceAfter != 7500 || w.Amount != 2500 || w.UserID != 1 {
		t.Fatalf("unexpected withdrawal: %+v", w)
	}

	u, err := svc.User(ctx, 1)
	if err != nil {
		t.Fatal(err)
	}
	if u.Balance != 7500 {
		t.Fatalf("balance = %d, want 7500", u.Balance)
	}

	history, err := svc.Withdrawals(ctx, 1, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(history) != 1 || history[0].ID != w.ID {
		t.Fatalf("unexpected history: %+v", history)
	}
}

func TestWithdrawErrors(t *testing.T) {
	svc, _ := setup(t, 100)
	ctx := context.Background()

	tests := []struct {
		name   string
		userID int64
		amount int64
		want   error
	}{
		{"insufficient funds", 1, 101, billing.ErrInsufficientFunds},
		{"unknown user", 999, 1, billing.ErrUserNotFound},
		{"zero amount", 1, 0, billing.ErrInvalidAmount},
		{"negative amount", 1, -5, billing.ErrInvalidAmount},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := svc.Withdraw(ctx, tt.userID, tt.amount); !errors.Is(err, tt.want) {
				t.Fatalf("err = %v, want %v", err, tt.want)
			}
		})
	}

	u, err := svc.User(ctx, 1)
	if err != nil {
		t.Fatal(err)
	}
	if u.Balance != 100 {
		t.Fatalf("balance changed after failed withdrawals: %d", u.Balance)
	}
}

func TestWithdrawConcurrent(t *testing.T) {
	const (
		balance  = 100000
		amount   = 3000
		attempts = 50
	)
	svc, pool := setup(t, balance)
	ctx := context.Background()

	var succeeded, rejected atomic.Int64
	var wg sync.WaitGroup
	for range attempts {
		wg.Go(func() {
			_, err := svc.Withdraw(ctx, 1, amount)
			switch {
			case err == nil:
				succeeded.Add(1)
			case errors.Is(err, billing.ErrInsufficientFunds):
				rejected.Add(1)
			default:
				t.Error(err)
			}
		})
	}
	wg.Wait()

	if want := int64(balance / amount); succeeded.Load() != want {
		t.Fatalf("succeeded = %d, want %d", succeeded.Load(), want)
	}
	if rejected.Load() != attempts-balance/amount {
		t.Fatalf("rejected = %d", rejected.Load())
	}

	u, err := svc.User(ctx, 1)
	if err != nil {
		t.Fatal(err)
	}
	if u.Balance != balance%amount {
		t.Fatalf("balance = %d, want %d", u.Balance, balance%amount)
	}

	// Every record's "before" must equal the previous record's "after".
	var broken int
	err = pool.QueryRow(ctx, `
		SELECT count(*) FROM (
			SELECT balance_before, lag(balance_after) OVER (ORDER BY id) AS prev_after
			FROM withdrawals WHERE user_id = 1
		) t WHERE prev_after IS NOT NULL AND prev_after <> balance_before`).Scan(&broken)
	if err != nil {
		t.Fatal(err)
	}
	if broken != 0 {
		t.Fatalf("%d history records break the balance chain", broken)
	}
}

func TestWithdrawalsUnknownUser(t *testing.T) {
	svc, _ := setup(t, 0)

	if _, err := svc.Withdrawals(context.Background(), 999, 10); !errors.Is(err, billing.ErrUserNotFound) {
		t.Fatalf("err = %v, want ErrUserNotFound", err)
	}
}

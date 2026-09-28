package billing

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

var (
	ErrUserNotFound      = errors.New("user not found")
	ErrInsufficientFunds = errors.New("insufficient funds")
	ErrInvalidAmount     = errors.New("amount must be positive")
)

// User is a wallet owner. Balance is in cents.
type User struct {
	ID      int64 `json:"id"`
	Balance int64 `json:"balance_cents"`
}

// Withdrawal is a single balance debit record. Amounts are in cents.
type Withdrawal struct {
	ID            int64     `json:"id"`
	UserID        int64     `json:"user_id"`
	Amount        int64     `json:"amount_cents"`
	BalanceBefore int64     `json:"balance_before_cents"`
	BalanceAfter  int64     `json:"balance_after_cents"`
	CreatedAt     time.Time `json:"created_at"`
}

// Service handles user balance operations.
type Service struct {
	db *pgxpool.Pool
}

func NewService(db *pgxpool.Pool) *Service {
	return &Service{db: db}
}

func (s *Service) User(ctx context.Context, userID int64) (User, error) {
	u := User{ID: userID}
	err := s.db.QueryRow(ctx, `SELECT balance FROM users WHERE id = $1`, userID).Scan(&u.Balance)
	if errors.Is(err, pgx.ErrNoRows) {
		return User{}, ErrUserNotFound
	}
	if err != nil {
		return User{}, fmt.Errorf("failed to get user: %w", err)
	}
	return u, nil
}

// Withdraw debits amount from the user's balance and records the operation.
//
// The conditional UPDATE takes a row lock and re-checks the balance under
// it, so concurrent withdrawals are serialized by Postgres and the balance
// can't go negative. Debit and history insert are a single statement, hence
// atomic without an explicit transaction.
func (s *Service) Withdraw(ctx context.Context, userID, amount int64) (Withdrawal, error) {
	if amount <= 0 {
		return Withdrawal{}, ErrInvalidAmount
	}

	const q = `
		WITH debited AS (
			UPDATE users
			SET balance = balance - $2
			WHERE id = $1 AND balance >= $2
			RETURNING id, balance
		)
		INSERT INTO withdrawals (user_id, amount, balance_before, balance_after)
		SELECT id, $2, balance + $2, balance FROM debited
		RETURNING id, user_id, amount, balance_before, balance_after, created_at`

	var w Withdrawal
	err := s.db.QueryRow(ctx, q, userID, amount).Scan(
		&w.ID, &w.UserID, &w.Amount, &w.BalanceBefore, &w.BalanceAfter, &w.CreatedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		// Nothing debited: either no such user or not enough money.
		if _, err := s.User(ctx, userID); err != nil {
			return Withdrawal{}, err
		}
		return Withdrawal{}, ErrInsufficientFunds
	}
	if err != nil {
		return Withdrawal{}, fmt.Errorf("failed to withdraw: %w", err)
	}
	return w, nil
}

// Withdrawals returns the user's withdrawal history, newest first.
func (s *Service) Withdrawals(ctx context.Context, userID int64, limit int) ([]Withdrawal, error) {
	const q = `
		SELECT id, user_id, amount, balance_before, balance_after, created_at
		FROM withdrawals
		WHERE user_id = $1
		ORDER BY id DESC
		LIMIT $2`

	rows, err := s.db.Query(ctx, q, userID, limit)
	if err != nil {
		return nil, fmt.Errorf("failed to query withdrawals: %w", err)
	}
	history, err := pgx.CollectRows(rows, pgx.RowToStructByPos[Withdrawal])
	if err != nil {
		return nil, fmt.Errorf("failed to scan withdrawals: %w", err)
	}

	if len(history) == 0 {
		if _, err := s.User(ctx, userID); err != nil {
			return nil, err
		}
	}
	return history, nil
}

package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

var (
	ErrArchivedSubscription = errors.New("subscription is archived")
	ErrInvalidRenewal       = errors.New("new expiry date must be after the renewal date")
)

type RenewalRecord struct {
	ID                int64
	SubscriptionID    sql.NullInt64
	SubscriptionRef   int64
	SubscriptionName  string
	RenewedAt         time.Time
	PreviousExpiresAt time.Time
	NewExpiresAt      time.Time
	Amount            sql.NullFloat64
	Currency          string
	Notes             string
	CreatedAt         time.Time
}

type ImportedSubscription struct {
	ReferenceID int64
	Item        Subscription
}

type ImportBundle struct {
	Subscriptions []ImportedSubscription
	Renewals      []RenewalRecord
}

func (s *Store) ListAllSubscriptions(ctx context.Context) ([]Subscription, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, name, category, provider, account_label, description, start_date,
		       expires_at, next_billing_at, renewal_cycle, custom_cycle_days, renewal_price, currency,
		       auto_renew, renewal_url, priority, tags, notes, archived, created_at, updated_at
		FROM subscriptions
		ORDER BY expires_at ASC, name ASC`)
	if err != nil {
		return nil, fmt.Errorf("query all subscriptions: %w", err)
	}
	defer rows.Close()
	return collectSubscriptions(rows)
}

func (s *Store) ListSubscriptionsBetween(ctx context.Context, start, end time.Time) ([]Subscription, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, name, category, provider, account_label, description, start_date,
		       expires_at, next_billing_at, renewal_cycle, custom_cycle_days, renewal_price, currency,
		       auto_renew, renewal_url, priority, tags, notes, archived, created_at, updated_at
		FROM subscriptions
		WHERE archived=false AND (expires_at BETWEEN $1::date AND $2::date OR next_billing_at BETWEEN $1::date AND $2::date)
		ORDER BY expires_at, name`, start.Format("2006-01-02"), end.Format("2006-01-02"))
	if err != nil {
		return nil, fmt.Errorf("query calendar subscriptions: %w", err)
	}
	defer rows.Close()
	return collectSubscriptions(rows)
}

func collectSubscriptions(rows *sql.Rows) ([]Subscription, error) {
	items := make([]Subscription, 0)
	for rows.Next() {
		item, err := scanSubscription(rows)
		if err != nil {
			return nil, fmt.Errorf("read subscription: %w", err)
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate subscriptions: %w", err)
	}
	return items, nil
}

func (s *Store) ListRenewals(ctx context.Context, limit int) ([]RenewalRecord, error) {
	if limit <= 0 {
		limit = 5000
	}
	if limit > 5001 {
		limit = 5001
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, subscription_id, subscription_name, renewed_at, previous_expires_at,
		       new_expires_at, amount, currency, notes, created_at
		FROM renewal_records
		ORDER BY renewed_at DESC, id DESC
		LIMIT $1`, limit)
	if err != nil {
		return nil, fmt.Errorf("query renewal history: %w", err)
	}
	defer rows.Close()
	items := make([]RenewalRecord, 0)
	for rows.Next() {
		var item RenewalRecord
		if err := rows.Scan(&item.ID, &item.SubscriptionID, &item.SubscriptionName, &item.RenewedAt,
			&item.PreviousExpiresAt, &item.NewExpiresAt, &item.Amount, &item.Currency, &item.Notes, &item.CreatedAt); err != nil {
			return nil, fmt.Errorf("read renewal history: %w", err)
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate renewal history: %w", err)
	}
	return items, nil
}

func (s *Store) RecordRenewal(ctx context.Context, subscriptionID int64, renewedAt, newExpiresAt time.Time, nextBillingAt sql.NullTime, amount sql.NullFloat64, currency, notes string) error {
	if !newExpiresAt.After(renewedAt) {
		return ErrInvalidRenewal
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin renewal: %w", err)
	}
	defer tx.Rollback()
	var name string
	var previousExpiresAt time.Time
	var archived bool
	if err := tx.QueryRowContext(ctx, `SELECT name, expires_at, archived FROM subscriptions WHERE id=$1 FOR UPDATE`, subscriptionID).
		Scan(&name, &previousExpiresAt, &archived); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return ErrNotFound
		}
		return fmt.Errorf("lock subscription for renewal: %w", err)
	}
	if archived {
		return ErrArchivedSubscription
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO renewal_records (
			subscription_id, subscription_name, renewed_at, previous_expires_at,
			new_expires_at, amount, currency, notes
		) VALUES ($1,$2,$3,$4,$5,$6,$7,$8)`, subscriptionID, name, renewedAt, previousExpiresAt,
		newExpiresAt, nullPrice(amount), currency, notes); err != nil {
		return fmt.Errorf("insert renewal history: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `
		UPDATE subscriptions SET expires_at=$2, next_billing_at=$3, updated_at=now()
		WHERE id=$1`, subscriptionID, newExpiresAt, nullDate(nextBillingAt)); err != nil {
		return fmt.Errorf("update renewed subscription: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit renewal: %w", err)
	}
	return nil
}

func (s *Store) ImportBundle(ctx context.Context, bundle ImportBundle) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin data import: %w", err)
	}
	defer tx.Rollback()
	ids := make(map[int64]int64, len(bundle.Subscriptions))
	for _, imported := range bundle.Subscriptions {
		if imported.ReferenceID <= 0 {
			return fmt.Errorf("subscription reference id must be positive")
		}
		if _, exists := ids[imported.ReferenceID]; exists {
			return fmt.Errorf("duplicate subscription reference id %d", imported.ReferenceID)
		}
		id, err := insertSubscription(ctx, tx, imported.Item)
		if err != nil {
			return err
		}
		ids[imported.ReferenceID] = id
	}
	for _, record := range bundle.Renewals {
		var subscriptionID any
		if record.SubscriptionRef > 0 {
			if id, ok := ids[record.SubscriptionRef]; ok {
				subscriptionID = id
			}
		}
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO renewal_records (
				subscription_id, subscription_name, renewed_at, previous_expires_at,
				new_expires_at, amount, currency, notes, created_at
			) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9)`, subscriptionID, record.SubscriptionName,
			record.RenewedAt, record.PreviousExpiresAt, record.NewExpiresAt, nullPrice(record.Amount),
			record.Currency, record.Notes, record.CreatedAt); err != nil {
			return fmt.Errorf("import renewal history: %w", err)
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit data import: %w", err)
	}
	return nil
}

func insertSubscription(ctx context.Context, tx *sql.Tx, item Subscription) (int64, error) {
	var id int64
	err := tx.QueryRowContext(ctx, `
		INSERT INTO subscriptions (
			name, category, provider, account_label, description, start_date, expires_at,
			next_billing_at, renewal_cycle, custom_cycle_days, renewal_price, currency, auto_renew,
			renewal_url, priority, tags, notes, archived
		) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18)
		RETURNING id`, item.Name, item.Category, item.Provider, item.AccountLabel, item.Description,
		nullDate(item.StartDate), item.ExpiresAt, nullDate(item.NextBillingAt), item.RenewalCycle,
		nullDays(item.CustomCycleDays), nullPrice(item.RenewalPrice), item.Currency, item.AutoRenew,
		item.RenewalURL, item.Priority, item.Tags, item.Notes, item.Archived).Scan(&id)
	if err != nil {
		return 0, fmt.Errorf("import subscription %q: %w", item.Name, err)
	}
	return id, nil
}

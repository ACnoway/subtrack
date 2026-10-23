package store

import (
	"context"
	"database/sql"
	"fmt"
	"time"
)

type Subscription struct {
	ID            int64
	Name          string
	Category      string
	Provider      string
	AccountLabel  string
	Description   string
	StartDate     sql.NullTime
	ExpiresAt     time.Time
	NextBillingAt sql.NullTime
	RenewalCycle  string
	RenewalPrice  sql.NullFloat64
	Currency      string
	AutoRenew     bool
	RenewalURL    string
	Priority      int
	Tags          string
	Notes         string
	Archived      bool
	CreatedAt     time.Time
	UpdatedAt     time.Time
}

func (s *Store) ListSubscriptions(ctx context.Context, search, status, category string) ([]Subscription, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, name, category, provider, account_label, description, start_date,
		       expires_at, next_billing_at, renewal_cycle, renewal_price, currency,
		       auto_renew, renewal_url, priority, tags, notes, archived, created_at, updated_at
		FROM subscriptions
		WHERE (name ILIKE '%' || $1 || '%' OR provider ILIKE '%' || $1 || '%' OR account_label ILIKE '%' || $1 || '%' OR tags ILIKE '%' || $1 || '%')
		  AND ($3 = '' OR category = $3)
		  AND (
		    ($2 = 'all' AND archived = false) OR
		    ($2 = 'archived' AND archived = true) OR
		    ($2 = 'expired' AND archived = false AND expires_at < CURRENT_DATE) OR
		    ($2 = 'upcoming' AND archived = false AND expires_at >= CURRENT_DATE AND expires_at <= CURRENT_DATE + 30) OR
		    ($2 = 'active' AND archived = false AND expires_at > CURRENT_DATE + 30)
		  )
		ORDER BY archived ASC, expires_at ASC, name ASC`, search, status, category)
	if err != nil {
		return nil, fmt.Errorf("query subscriptions: %w", err)
	}
	defer rows.Close()
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

func (s *Store) GetSubscription(ctx context.Context, id int64) (Subscription, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT id, name, category, provider, account_label, description, start_date,
		       expires_at, next_billing_at, renewal_cycle, renewal_price, currency,
		       auto_renew, renewal_url, priority, tags, notes, archived, created_at, updated_at
		FROM subscriptions WHERE id = $1`, id)
	item, err := scanSubscription(row)
	if err != nil {
		if err == sql.ErrNoRows {
			return Subscription{}, ErrNotFound
		}
		return Subscription{}, fmt.Errorf("get subscription: %w", err)
	}
	return item, nil
}

var ErrNotFound = fmt.Errorf("subscription not found")

func (s *Store) SaveSubscription(ctx context.Context, item Subscription) (int64, error) {
	if item.ID == 0 {
		err := s.db.QueryRowContext(ctx, `
			INSERT INTO subscriptions (
				name, category, provider, account_label, description, start_date, expires_at,
				next_billing_at, renewal_cycle, renewal_price, currency, auto_renew,
				renewal_url, priority, tags, notes
			) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16)
			RETURNING id`, item.Name, item.Category, item.Provider, item.AccountLabel, item.Description,
			nullDate(item.StartDate), item.ExpiresAt, nullDate(item.NextBillingAt), item.RenewalCycle,
			nullPrice(item.RenewalPrice), item.Currency, item.AutoRenew, item.RenewalURL, item.Priority,
			item.Tags, item.Notes).Scan(&item.ID)
		if err != nil {
			return 0, fmt.Errorf("insert subscription: %w", err)
		}
		return item.ID, nil
	}
	result, err := s.db.ExecContext(ctx, `
		UPDATE subscriptions SET name=$2, category=$3, provider=$4, account_label=$5,
			description=$6, start_date=$7, expires_at=$8, next_billing_at=$9,
			renewal_cycle=$10, renewal_price=$11, currency=$12, auto_renew=$13,
			renewal_url=$14, priority=$15, tags=$16, notes=$17, updated_at=now()
		WHERE id=$1`, item.ID, item.Name, item.Category, item.Provider, item.AccountLabel, item.Description,
		nullDate(item.StartDate), item.ExpiresAt, nullDate(item.NextBillingAt), item.RenewalCycle,
		nullPrice(item.RenewalPrice), item.Currency, item.AutoRenew, item.RenewalURL, item.Priority,
		item.Tags, item.Notes)
	if err != nil {
		return 0, fmt.Errorf("update subscription: %w", err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("check updated subscription: %w", err)
	}
	if affected == 0 {
		return 0, ErrNotFound
	}
	return item.ID, nil
}

func (s *Store) SetArchived(ctx context.Context, id int64, archived bool) error {
	result, err := s.db.ExecContext(ctx, `UPDATE subscriptions SET archived=$2, updated_at=now() WHERE id=$1`, id, archived)
	if err != nil {
		return fmt.Errorf("update archive status: %w", err)
	}
	count, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("check archive status: %w", err)
	}
	if count == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *Store) DeleteSubscription(ctx context.Context, id int64) error {
	result, err := s.db.ExecContext(ctx, `DELETE FROM subscriptions WHERE id=$1`, id)
	if err != nil {
		return fmt.Errorf("delete subscription: %w", err)
	}
	count, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("check deleted subscription: %w", err)
	}
	if count == 0 {
		return ErrNotFound
	}
	return nil
}

type scanner interface {
	Scan(dest ...any) error
}

func scanSubscription(row scanner) (Subscription, error) {
	var item Subscription
	err := row.Scan(&item.ID, &item.Name, &item.Category, &item.Provider, &item.AccountLabel,
		&item.Description, &item.StartDate, &item.ExpiresAt, &item.NextBillingAt,
		&item.RenewalCycle, &item.RenewalPrice, &item.Currency, &item.AutoRenew,
		&item.RenewalURL, &item.Priority, &item.Tags, &item.Notes, &item.Archived,
		&item.CreatedAt, &item.UpdatedAt)
	return item, err
}

func nullDate(value sql.NullTime) any {
	if !value.Valid {
		return nil
	}
	return value.Time
}

func nullPrice(value sql.NullFloat64) any {
	if !value.Valid {
		return nil
	}
	return value.Float64
}

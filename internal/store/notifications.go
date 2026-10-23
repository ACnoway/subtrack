package store

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"fmt"
	"strings"
	"time"
)

type NotificationSettings struct {
	Email           string
	EmailEnabled    bool
	TelegramChatID  string
	TelegramEnabled bool
	Timezone        string
	DeliveryTime    string
}

type ReminderRules struct {
	Default   []int
	Important []int
}

func (s *Store) GetNotificationSettings(ctx context.Context) (NotificationSettings, ReminderRules, error) {
	var settings NotificationSettings
	err := s.db.QueryRowContext(ctx, `
		SELECT email, email_enabled, telegram_chat_id, telegram_enabled, timezone,
		       to_char(delivery_time, 'HH24:MI')
		FROM notification_settings WHERE id = 1`).Scan(
		&settings.Email, &settings.EmailEnabled, &settings.TelegramChatID,
		&settings.TelegramEnabled, &settings.Timezone, &settings.DeliveryTime)
	if err != nil {
		return NotificationSettings{}, ReminderRules{}, fmt.Errorf("read notification settings: %w", err)
	}
	rows, err := s.db.QueryContext(ctx, `SELECT scope, days_before FROM reminder_rules WHERE enabled = true ORDER BY scope, days_before DESC`)
	if err != nil {
		return NotificationSettings{}, ReminderRules{}, fmt.Errorf("read reminder rules: %w", err)
	}
	defer rows.Close()
	var rules ReminderRules
	for rows.Next() {
		var scope string
		var days int
		if err := rows.Scan(&scope, &days); err != nil {
			return NotificationSettings{}, ReminderRules{}, fmt.Errorf("scan reminder rule: %w", err)
		}
		if scope == "important" {
			rules.Important = append(rules.Important, days)
		} else {
			rules.Default = append(rules.Default, days)
		}
	}
	if err := rows.Err(); err != nil {
		return NotificationSettings{}, ReminderRules{}, fmt.Errorf("iterate reminder rules: %w", err)
	}
	return settings, rules, nil
}

func (s *Store) CreateTelegramBinding(ctx context.Context, token string, expiresAt time.Time) error {
	hash := tokenHash(token)
	_, err := s.db.ExecContext(ctx, `
		UPDATE notification_settings SET telegram_bind_token_hash=$1,
		telegram_bind_expires_at=$2, updated_at=now() WHERE id=1`, hash, expiresAt)
	if err != nil {
		return fmt.Errorf("create Telegram binding: %w", err)
	}
	return nil
}

func (s *Store) CompleteTelegramBinding(ctx context.Context, token, chatID string) (bool, error) {
	result, err := s.db.ExecContext(ctx, `
		UPDATE notification_settings
		SET telegram_chat_id=$2, telegram_bind_token_hash=NULL,
		    telegram_bind_expires_at=NULL, updated_at=now()
		WHERE id=1 AND telegram_bind_token_hash=$1 AND telegram_bind_expires_at > now()`, tokenHash(token), chatID)
	if err != nil {
		return false, fmt.Errorf("complete Telegram binding: %w", err)
	}
	count, err := result.RowsAffected()
	if err != nil {
		return false, fmt.Errorf("check Telegram binding: %w", err)
	}
	return count == 1, nil
}

func (s *Store) UnbindTelegram(ctx context.Context) error {
	_, err := s.db.ExecContext(ctx, `
		UPDATE notification_settings SET telegram_chat_id='', telegram_enabled=false,
		telegram_bind_token_hash=NULL, telegram_bind_expires_at=NULL, updated_at=now() WHERE id=1`)
	if err != nil {
		return fmt.Errorf("unbind Telegram: %w", err)
	}
	return nil
}

func (s *Store) TelegramUpdateOffset(ctx context.Context) (int64, error) {
	var offset int64
	err := s.db.QueryRowContext(ctx, `SELECT telegram_update_offset FROM notification_settings WHERE id=1`).Scan(&offset)
	if err != nil {
		return 0, fmt.Errorf("read Telegram update offset: %w", err)
	}
	return offset, nil
}

func (s *Store) SaveTelegramUpdateOffset(ctx context.Context, offset int64) error {
	_, err := s.db.ExecContext(ctx, `UPDATE notification_settings SET telegram_update_offset=GREATEST(telegram_update_offset,$1) WHERE id=1`, offset)
	if err != nil {
		return fmt.Errorf("save Telegram update offset: %w", err)
	}
	return nil
}

func (s *Store) TouchNotificationWorker(ctx context.Context, seenAt time.Time) error {
	_, err := s.db.ExecContext(ctx, `UPDATE notification_worker_state SET last_seen_at=$1 WHERE id=1`, seenAt)
	if err != nil {
		return fmt.Errorf("update notification worker heartbeat: %w", err)
	}
	return nil
}

func (s *Store) NotificationWorkerLastSeen(ctx context.Context) (time.Time, error) {
	var seenAt time.Time
	err := s.db.QueryRowContext(ctx, `SELECT last_seen_at FROM notification_worker_state WHERE id=1`).Scan(&seenAt)
	if err != nil {
		return time.Time{}, fmt.Errorf("read notification worker heartbeat: %w", err)
	}
	return seenAt, nil
}

func tokenHash(token string) string {
	digest := sha256.Sum256([]byte(token))
	return hex.EncodeToString(digest[:])
}

type ReminderSubscription struct {
	ID           int64
	Name         string
	Category     string
	Provider     string
	Priority     int
	ExpiresAt    time.Time
	RenewalPrice sql.NullFloat64
	Currency     string
	AutoRenew    bool
	RenewalURL   string
}

func (s *Store) ListReminderSubscriptions(ctx context.Context, from, through time.Time) ([]ReminderSubscription, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, name, category, provider, priority, expires_at, renewal_price,
		       currency, auto_renew, renewal_url
		FROM subscriptions
		WHERE archived=false AND expires_at BETWEEN $1::date AND $2::date
		ORDER BY expires_at, id`, from.Format("2006-01-02"), through.Format("2006-01-02"))
	if err != nil {
		return nil, fmt.Errorf("list reminder subscriptions: %w", err)
	}
	defer rows.Close()
	items := make([]ReminderSubscription, 0)
	for rows.Next() {
		var item ReminderSubscription
		if err := rows.Scan(&item.ID, &item.Name, &item.Category, &item.Provider, &item.Priority,
			&item.ExpiresAt, &item.RenewalPrice, &item.Currency, &item.AutoRenew, &item.RenewalURL); err != nil {
			return nil, fmt.Errorf("scan reminder subscription: %w", err)
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate reminder subscriptions: %w", err)
	}
	return items, nil
}

func (s *Store) InsertReminderJob(ctx context.Context, item ReminderSubscription, targetDate time.Time, daysBefore int, channel string, scheduledAt time.Time) error {
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO notification_jobs (subscription_id, subscription_name, target_date, reminder_offset, channel, scheduled_at)
		VALUES ($1,$2,$3::date,$4,$5,$6)
		ON CONFLICT (subscription_id, target_date, reminder_offset, channel) DO UPDATE
		SET subscription_name=EXCLUDED.subscription_name,
		    status=CASE WHEN notification_jobs.status='cancelled' THEN 'pending' ELSE notification_jobs.status END,
		    scheduled_at=CASE WHEN notification_jobs.status='cancelled' THEN EXCLUDED.scheduled_at ELSE notification_jobs.scheduled_at END,
		    attempt_count=CASE WHEN notification_jobs.status='cancelled' THEN 0 ELSE notification_jobs.attempt_count END,
		    last_error=CASE WHEN notification_jobs.status='cancelled' THEN '' ELSE notification_jobs.last_error END,
		    sent_at=CASE WHEN notification_jobs.status='cancelled' THEN NULL ELSE notification_jobs.sent_at END,
		    updated_at=now()
		WHERE notification_jobs.status='cancelled'
		   OR notification_jobs.subscription_name IS DISTINCT FROM EXCLUDED.subscription_name`,
		item.ID, item.Name, targetDate.Format("2006-01-02"), daysBefore, channel, scheduledAt)
	if err != nil {
		return fmt.Errorf("schedule reminder job: %w", err)
	}
	return nil
}

func (s *Store) CancelObsoleteReminderJobs(ctx context.Context, settings NotificationSettings) error {
	_, err := s.db.ExecContext(ctx, `
		UPDATE notification_jobs j
		SET status='cancelled', updated_at=now(), last_error='提醒设置、资产日期或资产状态已变更'
		WHERE j.status='pending' AND (
			NOT EXISTS (
				SELECT 1 FROM subscriptions s
				WHERE s.id=j.subscription_id AND s.archived=false AND s.expires_at=j.target_date
			)
			OR (j.channel='email' AND (NOT $1 OR $3=''))
			OR (j.channel='telegram' AND (NOT $2 OR $4=''))
			OR NOT EXISTS (
				SELECT 1 FROM reminder_rules r
				JOIN subscriptions s ON s.id=j.subscription_id
				WHERE r.enabled=true AND r.days_before=j.reminder_offset
				  AND (r.scope='default' OR (r.scope='important' AND (s.priority=3 OR s.category IN ('domain','certificate'))))
			)
		)`, settings.EmailEnabled, settings.TelegramEnabled, settings.Email, settings.TelegramChatID)
	if err != nil {
		return fmt.Errorf("cancel obsolete reminder jobs: %w", err)
	}
	return nil
}

func (s *Store) ReschedulePendingReminderJobs(ctx context.Context) error {
	_, err := s.db.ExecContext(ctx, `
		UPDATE notification_jobs j
		SET scheduled_at=((j.target_date - j.reminder_offset) + n.delivery_time) AT TIME ZONE n.timezone,
		    updated_at=now()
		FROM notification_settings n
		WHERE n.id=1 AND j.status='pending' AND j.attempt_count=0
		  AND j.scheduled_at IS DISTINCT FROM ((j.target_date - j.reminder_offset) + n.delivery_time) AT TIME ZONE n.timezone`)
	if err != nil {
		return fmt.Errorf("reschedule pending reminder jobs: %w", err)
	}
	return nil
}

type NotificationJob struct {
	ID               int64
	SubscriptionID   int64
	SubscriptionName string
	TargetDate       time.Time
	ReminderOffset   int
	Channel          string
	ScheduledAt      time.Time
	AttemptCount     int
	Message          ReminderSubscription
}

func (s *Store) ClaimDueNotificationJob(ctx context.Context, now time.Time) (*NotificationJob, error) {
	if _, err := s.db.ExecContext(ctx, `
		WITH recovered AS (
			UPDATE notification_jobs
			SET status=CASE WHEN attempt_count >= 5 THEN 'failed' ELSE 'pending' END,
			    scheduled_at=CASE WHEN attempt_count >= 5 THEN scheduled_at ELSE $1 END,
			    updated_at=now(), last_error='Worker 在发送过程中重启，任务已恢复'
			WHERE status='sending' AND updated_at < $1 - interval '5 minutes'
			RETURNING id, subscription_id, subscription_name, reminder_offset, channel,
			          scheduled_at, status, attempt_count, last_error
		)
		INSERT INTO notification_logs (job_id, subscription_id, subscription_name, reminder_offset,
			channel, scheduled_at, attempted_at, result, attempt_count, error_message)
		SELECT id, subscription_id, subscription_name, reminder_offset, channel, scheduled_at, $1,
		       CASE WHEN status='failed' THEN 'failed' ELSE 'retrying' END, attempt_count, last_error
		FROM recovered`, now); err != nil {
		return nil, fmt.Errorf("recover interrupted notification jobs: %w", err)
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("begin notification claim: %w", err)
	}
	defer tx.Rollback()
	var job NotificationJob
	var item ReminderSubscription
	err = tx.QueryRowContext(ctx, `
		SELECT j.id, j.subscription_id, j.subscription_name, j.target_date,
		       j.reminder_offset, j.channel, j.scheduled_at, j.attempt_count,
		       s.id, s.name, s.category, s.provider, s.priority, s.expires_at,
		       s.renewal_price, s.currency, s.auto_renew, s.renewal_url
		FROM notification_jobs j
		JOIN subscriptions s ON s.id=j.subscription_id
		WHERE j.status='pending' AND j.scheduled_at <= $1
		  AND j.attempt_count < 5 AND s.archived=false AND s.expires_at=j.target_date
		ORDER BY j.scheduled_at, j.id
		LIMIT 1 FOR UPDATE OF j SKIP LOCKED`, now).Scan(
		&job.ID, &job.SubscriptionID, &job.SubscriptionName, &job.TargetDate,
		&job.ReminderOffset, &job.Channel, &job.ScheduledAt, &job.AttemptCount,
		&item.ID, &item.Name, &item.Category, &item.Provider, &item.Priority, &item.ExpiresAt,
		&item.RenewalPrice, &item.Currency, &item.AutoRenew, &item.RenewalURL)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("select due notification job: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `UPDATE notification_jobs SET status='sending', attempt_count=attempt_count+1, updated_at=now() WHERE id=$1`, job.ID); err != nil {
		return nil, fmt.Errorf("claim notification job: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit notification claim: %w", err)
	}
	job.AttemptCount++
	job.Message = item
	return &job, nil
}

func (s *Store) FinishNotificationJob(ctx context.Context, job NotificationJob, sendErr error, attemptedAt time.Time) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin notification result update: %w", err)
	}
	defer tx.Rollback()
	result := "sent"
	errorMessage := ""
	status := "sent"
	nextScheduled := any(nil)
	if sendErr != nil {
		errorMessage = truncate(strings.TrimSpace(sendErr.Error()), 1000)
		status = "failed"
		result = "failed"
		if job.AttemptCount < 5 {
			status = "pending"
			result = "retrying"
			delays := []time.Duration{time.Minute, 5 * time.Minute, 15 * time.Minute, time.Hour}
			nextScheduled = attemptedAt.Add(delays[min(job.AttemptCount-1, len(delays)-1)])
		}
	}
	_, err = tx.ExecContext(ctx, `
		UPDATE notification_jobs SET status=$2, scheduled_at=COALESCE($3, scheduled_at),
		last_error=$4, sent_at=CASE WHEN $2='sent' THEN $5 ELSE sent_at END, updated_at=now()
		WHERE id=$1`, job.ID, status, nextScheduled, errorMessage, attemptedAt)
	if err != nil {
		return fmt.Errorf("update notification job result: %w", err)
	}
	_, err = tx.ExecContext(ctx, `
		INSERT INTO notification_logs (job_id, subscription_id, subscription_name, reminder_offset,
		channel, scheduled_at, attempted_at, result, attempt_count, error_message)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)`, job.ID, job.SubscriptionID,
		job.SubscriptionName, job.ReminderOffset, job.Channel, job.ScheduledAt,
		attemptedAt, result, job.AttemptCount, errorMessage)
	if err != nil {
		return fmt.Errorf("record notification attempt: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit notification result: %w", err)
	}
	return nil
}

func (s *Store) RecordTestNotification(ctx context.Context, channel, result, errorMessage string, attemptedAt time.Time) error {
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO notification_logs (subscription_name, channel, scheduled_at, attempted_at, result, attempt_count, error_message)
		VALUES ('通知测试', $1, $2, $2, $3, 1, $4)`, channel, attemptedAt, result, truncate(errorMessage, 1000))
	if err != nil {
		return fmt.Errorf("record test notification: %w", err)
	}
	return nil
}

type NotificationLog struct {
	ID               int64
	JobID            sql.NullInt64
	SubscriptionID   sql.NullInt64
	SubscriptionName string
	ReminderOffset   int
	Channel          string
	ScheduledAt      time.Time
	AttemptedAt      time.Time
	Result           string
	AttemptCount     int
	ErrorMessage     string
}

func (s *Store) ListNotificationLogs(ctx context.Context, limit int) ([]NotificationLog, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT l.id, l.job_id, l.subscription_id, l.subscription_name, l.reminder_offset,
		       l.channel, l.scheduled_at, l.attempted_at, l.result, l.attempt_count, l.error_message
		FROM notification_logs l ORDER BY l.attempted_at DESC, l.id DESC LIMIT $1`, limit)
	if err != nil {
		return nil, fmt.Errorf("list notification history: %w", err)
	}
	defer rows.Close()
	items := make([]NotificationLog, 0)
	for rows.Next() {
		var item NotificationLog
		if err := rows.Scan(&item.ID, &item.JobID, &item.SubscriptionID, &item.SubscriptionName,
			&item.ReminderOffset, &item.Channel, &item.ScheduledAt, &item.AttemptedAt,
			&item.Result, &item.AttemptCount, &item.ErrorMessage); err != nil {
			return nil, fmt.Errorf("scan notification history: %w", err)
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate notification history: %w", err)
	}
	return items, nil
}

func (s *Store) RetryNotificationJob(ctx context.Context, id int64) error {
	result, err := s.db.ExecContext(ctx, `
		UPDATE notification_jobs SET status='pending', scheduled_at=now(), attempt_count=0,
		last_error='', sent_at=NULL, updated_at=now()
		WHERE id=$1 AND status='failed' AND subscription_id IS NOT NULL`, id)
	if err != nil {
		return fmt.Errorf("retry notification job: %w", err)
	}
	count, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("check notification retry: %w", err)
	}
	if count == 0 {
		return ErrNotFound
	}
	return nil
}

func truncate(value string, maxRunes int) string {
	runes := []rune(value)
	if len(runes) > maxRunes {
		return string(runes[:maxRunes])
	}
	return value
}

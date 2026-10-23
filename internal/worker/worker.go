package worker

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/ACnoway/subtrack/internal/app"
	"github.com/ACnoway/subtrack/internal/notify"
	"github.com/ACnoway/subtrack/internal/store"
)

const maxReminderOffset = 365

type Worker struct {
	store  *store.Store
	cfg    app.Config
	logger *slog.Logger
}

func New(database *store.Store, cfg app.Config, logger *slog.Logger) *Worker {
	return &Worker{store: database, cfg: cfg, logger: logger}
}

func (w *Worker) Run(ctx context.Context) error {
	if err := w.touchHeartbeat(ctx); err != nil {
		return err
	}
	go w.heartbeatLoop(ctx)
	go w.pollTelegramUpdates(ctx)
	if err := w.processCycle(ctx); err != nil {
		w.logger.Error("notification worker cycle failed", "error", err)
	}
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			if err := w.processCycle(ctx); err != nil {
				w.logger.Error("notification worker cycle failed", "error", err)
			}
		}
	}
}

func (w *Worker) heartbeatLoop(ctx context.Context) {
	ticker := time.NewTicker(15 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := w.touchHeartbeat(ctx); err != nil {
				w.logger.Warn("notification worker heartbeat failed", "error", err)
			}
		}
	}
}

func (w *Worker) touchHeartbeat(parent context.Context) error {
	ctx, cancel := context.WithTimeout(parent, 3*time.Second)
	defer cancel()
	return w.store.TouchNotificationWorker(ctx, time.Now())
}

func (w *Worker) processCycle(ctx context.Context) error {
	settings, rules, err := w.store.GetNotificationSettings(ctx)
	if err != nil {
		return err
	}
	integrations, err := w.store.GetIntegrationSettings(ctx)
	if err != nil {
		return err
	}
	zone, err := time.LoadLocation(settings.Timezone)
	if err != nil {
		return fmt.Errorf("load notification timezone: %w", err)
	}
	now := time.Now().In(zone)
	today := dateAt(now, zone)
	deliverySettings := settings
	if !smtpConfig(integrations).Configured() {
		deliverySettings.EmailEnabled = false
	}
	if strings.TrimSpace(integrations.TelegramBotToken) == "" {
		deliverySettings.TelegramEnabled = false
	}
	if err := w.store.CancelObsoleteReminderJobs(ctx, deliverySettings); err != nil {
		return err
	}
	if err := w.scheduleJobs(ctx, deliverySettings, rules, today, zone); err != nil {
		return err
	}
	if err := w.store.ReschedulePendingReminderJobs(ctx); err != nil {
		return err
	}
	for {
		job, err := w.store.ClaimDueNotificationJob(ctx, time.Now())
		if err != nil {
			return err
		}
		if job == nil {
			return nil
		}
		sendCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
		sendErr := w.send(sendCtx, settings, integrations, *job)
		cancel()
		finishCtx, finishCancel := context.WithTimeout(context.Background(), 5*time.Second)
		finishErr := w.store.FinishNotificationJob(finishCtx, *job, sendErr, time.Now())
		finishCancel()
		if finishErr != nil {
			return finishErr
		}
		if sendErr != nil {
			w.logger.Warn("notification send failed", "job_id", job.ID, "channel", job.Channel, "attempt", job.AttemptCount, "error", sendErr)
		} else {
			w.logger.Info("notification sent", "job_id", job.ID, "channel", job.Channel)
		}
	}
}

func (w *Worker) scheduleJobs(ctx context.Context, settings store.NotificationSettings, rules store.ReminderRules, today time.Time, zone *time.Location) error {
	clock, err := time.Parse("15:04", settings.DeliveryTime)
	if err != nil {
		return fmt.Errorf("parse notification delivery time: %w", err)
	}
	from := today.AddDate(0, 0, -maxReminderOffset)
	through := today.AddDate(0, 0, 2*maxReminderOffset)
	items, err := w.store.ListReminderSubscriptions(ctx, from, through)
	if err != nil {
		return err
	}
	for _, planned := range planReminderJobs(items, rules, today, zone, clock.Hour(), clock.Minute()) {
		if settings.EmailEnabled && settings.Email != "" {
			if err := w.store.InsertReminderJob(ctx, planned.item, planned.targetDate, planned.offset, "email", planned.scheduledAt); err != nil {
				return err
			}
		}
		if settings.TelegramEnabled && settings.TelegramChatID != "" {
			if err := w.store.InsertReminderJob(ctx, planned.item, planned.targetDate, planned.offset, "telegram", planned.scheduledAt); err != nil {
				return err
			}
		}
	}
	return nil
}

type plannedReminder struct {
	item        store.ReminderSubscription
	targetDate  time.Time
	offset      int
	scheduledAt time.Time
}

func planReminderJobs(items []store.ReminderSubscription, rules store.ReminderRules, today time.Time, zone *time.Location, hour, minute int) []plannedReminder {
	windowEnd := today.AddDate(0, 0, maxReminderOffset)
	planned := make([]plannedReminder, 0)
	for _, item := range items {
		expiresDate := time.Date(item.ExpiresAt.Year(), item.ExpiresAt.Month(), item.ExpiresAt.Day(), 0, 0, 0, 0, zone)
		important := item.Priority == 3 || item.Category == "domain" || item.Category == "certificate"
		offsets := make(map[int]struct{}, len(rules.Default)+len(rules.Important))
		for _, value := range rules.Default {
			offsets[value] = struct{}{}
		}
		if important {
			for _, value := range rules.Important {
				offsets[value] = struct{}{}
			}
		}
		orderedOffsets := make([]int, 0, len(offsets))
		for value := range offsets {
			orderedOffsets = append(orderedOffsets, value)
		}
		sort.Ints(orderedOffsets)
		for _, offset := range orderedOffsets {
			alertDate := expiresDate.AddDate(0, 0, -offset)
			if alertDate.Before(today) || alertDate.After(windowEnd) {
				continue
			}
			scheduledAt := time.Date(alertDate.Year(), alertDate.Month(), alertDate.Day(), hour, minute, 0, 0, zone)
			planned = append(planned, plannedReminder{item: item, targetDate: expiresDate, offset: offset, scheduledAt: scheduledAt})
		}
	}
	return planned
}

func (w *Worker) send(ctx context.Context, settings store.NotificationSettings, integrations store.IntegrationSettings, job store.NotificationJob) error {
	message := makeReminder(job.Message, job.ReminderOffset, w.cfg.PublicURL)
	switch job.Channel {
	case "email":
		return notify.SendReminderEmail(ctx, smtpConfig(integrations), settings.Email, message)
	case "telegram":
		return notify.NewTelegramBot(integrations.TelegramBotToken).SendReminder(ctx, settings.TelegramChatID, message)
	default:
		return fmt.Errorf("unknown notification channel")
	}
}

func makeReminder(item store.ReminderSubscription, daysRemaining int, publicURL string) notify.Reminder {
	price := "未设置"
	if item.RenewalPrice.Valid {
		price = fmt.Sprintf("%s %.2f", strings.TrimSpace(item.Currency), item.RenewalPrice.Float64)
	}
	return notify.Reminder{
		Name: item.Name, Provider: item.Provider, ExpiresAt: item.ExpiresAt,
		DaysRemaining: daysRemaining, RenewalPrice: price, AutoRenew: item.AutoRenew,
		RenewalURL: item.RenewalURL, PublicURL: publicURL,
	}
}

func smtpConfig(integrations store.IntegrationSettings) notify.SMTPConfig {
	return notify.SMTPConfig{
		Host: integrations.SMTPHost, Port: integrations.SMTPPort, Username: integrations.SMTPUsername,
		Password: integrations.SMTPPassword, From: integrations.SMTPFrom, Encryption: integrations.SMTPEncryption,
	}
}

func dateAt(value time.Time, zone *time.Location) time.Time {
	local := value.In(zone)
	return time.Date(local.Year(), local.Month(), local.Day(), 0, 0, 0, 0, zone)
}

func (w *Worker) pollTelegramUpdates(ctx context.Context) {
	for ctx.Err() == nil {
		integrations, err := w.store.GetIntegrationSettings(ctx)
		if err != nil {
			w.logger.Error("read Telegram integration settings failed", "error", err)
			if !pause(ctx, 3*time.Second) {
				return
			}
			continue
		}
		if strings.TrimSpace(integrations.TelegramBotToken) == "" {
			if !pause(ctx, 5*time.Second) {
				return
			}
			continue
		}
		bot := notify.NewTelegramBot(integrations.TelegramBotToken)
		offset, err := w.store.TelegramUpdateOffset(ctx)
		if err != nil {
			w.logger.Error("read Telegram update offset failed", "error", err)
			if !pause(ctx, 3*time.Second) {
				return
			}
			continue
		}
		updates, err := bot.GetUpdates(ctx, offset)
		if err != nil {
			if ctx.Err() != nil {
				return
			}
			w.logger.Warn("Telegram update polling failed", "error", err)
			if !pause(ctx, 3*time.Second) {
				return
			}
			continue
		}
		for _, update := range updates {
			w.processTelegramUpdate(ctx, bot, update)
			if err := w.store.SaveTelegramUpdateOffset(ctx, update.UpdateID+1); err != nil {
				w.logger.Error("save Telegram update offset failed", "error", err)
				break
			}
		}
	}
}

func (w *Worker) processTelegramUpdate(ctx context.Context, bot *notify.TelegramBot, update notify.TelegramUpdate) {
	if update.Message == nil || update.Message.Chat.ID <= 0 {
		return
	}
	fields := strings.Fields(update.Message.Text)
	if len(fields) != 2 || (fields[0] != "/start" && !strings.HasPrefix(fields[0], "/start@")) {
		return
	}
	token := strings.TrimSpace(fields[1])
	if token == "" || len(token) > 64 {
		return
	}
	chatID := strconv.FormatInt(update.Message.Chat.ID, 10)
	bound, err := w.store.CompleteTelegramBinding(ctx, token, chatID)
	if err != nil {
		w.logger.Error("complete Telegram binding failed", "error", err)
		return
	}
	if err := bot.ReplyBinding(ctx, chatID, bound); err != nil {
		w.logger.Warn("send Telegram binding reply failed", "error", err)
	}
}

func pause(ctx context.Context, delay time.Duration) bool {
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}

func RunProcess(database *store.Store, cfg app.Config, logger *slog.Logger) error {
	worker := New(database, cfg, logger)
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	err := worker.Run(ctx)
	if errors.Is(err, context.Canceled) {
		return nil
	}
	return err
}

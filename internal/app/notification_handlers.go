package app

import (
	"context"
	"fmt"
	"html/template"
	"net/http"
	"net/mail"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/ACnoway/subtrack/internal/notify"
	"github.com/ACnoway/subtrack/internal/store"
)

type settingsPageData struct {
	Title              string
	CSRF               string
	Error              string
	Success            string
	Settings           store.NotificationSettings
	DefaultRules       string
	ImportantRules     string
	EmailConfigured    bool
	TelegramConfigured bool
	BotUsername        string
	BindingURL         string
	BindingExpiresAt   string
}

type notificationLogRow struct {
	Item         store.NotificationLog
	ChannelLabel string
	ResultLabel  string
	OffsetLabel  string
	Scheduled    string
	Attempted    string
	Retryable    bool
}

type notificationsPageData struct {
	Title   string
	CSRF    string
	Success string
	Error   string
	Items   []notificationLogRow
}

func (s *Server) notificationSettings(w http.ResponseWriter, r *http.Request) {
	settings, rules, err := s.store.GetNotificationSettings(r.Context())
	if err != nil {
		s.serverError(w, r, err)
		return
	}
	data := s.settingsData(r, settings, rules)
	switch r.URL.Query().Get("saved") {
	case "1":
		data.Success = "提醒设置已保存。"
	case "unbound":
		data.Success = "Telegram 已解除绑定。"
	}
	s.renderSettings(w, data, 200)
}

func (s *Server) saveNotificationSettings(w http.ResponseWriter, r *http.Request) {
	current, _, err := s.store.GetNotificationSettings(r.Context())
	if err != nil {
		s.serverError(w, r, err)
		return
	}
	settings := store.NotificationSettings{
		Email:           strings.TrimSpace(r.FormValue("email")),
		EmailEnabled:    r.FormValue("email_enabled") == "on",
		TelegramChatID:  current.TelegramChatID,
		TelegramEnabled: r.FormValue("telegram_enabled") == "on",
		Timezone:        strings.TrimSpace(r.FormValue("timezone")),
		DeliveryTime:    strings.TrimSpace(r.FormValue("delivery_time")),
	}
	rules, err := parseReminderRules(r.FormValue("default_rules"), r.FormValue("important_rules"))
	if err == nil {
		err = validateNotificationSettings(settings, s.cfg.TelegramBotToken != "")
	}
	if err != nil {
		data := s.settingsData(r, settings, rules)
		data.DefaultRules = r.FormValue("default_rules")
		data.ImportantRules = r.FormValue("important_rules")
		data.Error = err.Error()
		s.renderSettings(w, data, 422)
		return
	}
	if err := s.store.SaveNotificationSettings(r.Context(), settings, rules); err != nil {
		s.serverError(w, r, err)
		return
	}
	httpRedirect(w, r, "/settings?saved=1")
}

func validateNotificationSettings(settings store.NotificationSettings, telegramConfigured bool) error {
	if settings.Email != "" {
		parsed, err := mail.ParseAddress(settings.Email)
		if err != nil || parsed.Address != settings.Email {
			return fmt.Errorf("请填写有效的通知邮箱地址。")
		}
	}
	if settings.EmailEnabled && settings.Email == "" {
		return fmt.Errorf("启用邮件通知前请填写通知邮箱。")
	}
	if settings.TelegramEnabled && (settings.TelegramChatID == "" || strings.TrimSpace(settings.TelegramChatID) == "") {
		return fmt.Errorf("启用 Telegram 通知前请先绑定 Telegram 账号。")
	}
	if settings.TelegramEnabled && !telegramConfigured {
		return fmt.Errorf("请先在环境变量中配置 TELEGRAM_BOT_TOKEN。")
	}
	if _, err := time.LoadLocation(settings.Timezone); err != nil {
		return fmt.Errorf("时区无效，请填写 IANA 时区，例如 Asia/Shanghai。")
	}
	if _, err := time.Parse("15:04", settings.DeliveryTime); err != nil {
		return fmt.Errorf("发送时间无效，请使用 24 小时制 HH:MM。")
	}
	return nil
}

func parseReminderRules(defaultValue, importantValue string) (store.ReminderRules, error) {
	defaults, err := parseOffsets(defaultValue)
	if err != nil {
		return store.ReminderRules{}, fmt.Errorf("常规提醒天数无效：%w", err)
	}
	important, err := parseOffsets(importantValue)
	if err != nil {
		return store.ReminderRules{}, fmt.Errorf("重要资产提醒天数无效：%w", err)
	}
	return store.ReminderRules{Default: defaults, Important: important}, nil
}

func parseOffsets(value string) ([]int, error) {
	parts := strings.FieldsFunc(value, func(r rune) bool { return r == ',' || r == ';' || r == ' ' || r == '\n' || r == '\t' })
	seen := make(map[int]struct{}, len(parts))
	result := make([]int, 0, len(parts))
	for _, part := range parts {
		days, err := strconv.Atoi(part)
		if err != nil || days < -365 || days > 365 {
			return nil, fmt.Errorf("每项必须是 -365 到 365 的整数")
		}
		if _, exists := seen[days]; exists {
			return nil, fmt.Errorf("天数不能重复")
		}
		seen[days] = struct{}{}
		result = append(result, days)
	}
	if len(result) > 30 {
		return nil, fmt.Errorf("每组最多设置 30 个提醒时间")
	}
	sort.Ints(result)
	return result, nil
}

func (s *Server) createTelegramBinding(w http.ResponseWriter, r *http.Request) {
	settings, rules, err := s.store.GetNotificationSettings(r.Context())
	if err != nil {
		s.serverError(w, r, err)
		return
	}
	data := s.settingsData(r, settings, rules)
	if s.cfg.TelegramBotToken == "" {
		data.Error = "请先在 .env 中配置 TELEGRAM_BOT_TOKEN，并重启应用和 Worker。"
		s.renderSettings(w, data, 422)
		return
	}
	username := s.cfg.TelegramBotUsername
	if username == "" {
		ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
		username, err = notify.NewTelegramBot(s.cfg.TelegramBotToken).GetMe(ctx)
		cancel()
		if err != nil {
			data.Error = "无法读取 Telegram Bot 信息，请检查 TELEGRAM_BOT_TOKEN。"
			s.renderSettings(w, data, 502)
			return
		}
	}
	token, err := randomToken()
	if err != nil {
		s.serverError(w, r, err)
		return
	}
	expires := time.Now().Add(15 * time.Minute)
	if err := s.store.CreateTelegramBinding(r.Context(), token, expires); err != nil {
		s.serverError(w, r, err)
		return
	}
	data.BotUsername = strings.TrimPrefix(username, "@")
	data.BindingURL = "https://t.me/" + url.PathEscape(data.BotUsername) + "?start=" + url.QueryEscape(token)
	data.BindingExpiresAt = expires.Format("2006-01-02 15:04:05 MST")
	data.Success = "绑定链接已生成，请在 15 分钟内打开并向机器人发送 /start。"
	s.renderSettings(w, data, 200)
}

func (s *Server) unbindTelegram(w http.ResponseWriter, r *http.Request) {
	if err := s.store.UnbindTelegram(r.Context()); err != nil {
		s.serverError(w, r, err)
		return
	}
	httpRedirect(w, r, "/settings?saved=unbound")
}

func (s *Server) testEmailNotification(w http.ResponseWriter, r *http.Request) {
	settings, _, err := s.store.GetNotificationSettings(r.Context())
	if err != nil {
		s.serverError(w, r, err)
		return
	}
	attempted := time.Now()
	ctx, cancel := context.WithTimeout(r.Context(), 25*time.Second)
	err = notify.SendReminderEmail(ctx, s.smtpConfig(), settings.Email, notify.Reminder{
		Name: "SubTrack 通知测试", ExpiresAt: attempted.AddDate(0, 0, 7),
		DaysRemaining: 7, RenewalPrice: "测试消息", PublicURL: s.cfg.PublicURL,
	})
	cancel()
	s.recordTest(w, r, "email", attempted, err)
}

func (s *Server) testTelegramNotification(w http.ResponseWriter, r *http.Request) {
	settings, _, err := s.store.GetNotificationSettings(r.Context())
	if err != nil {
		s.serverError(w, r, err)
		return
	}
	attempted := time.Now()
	var sendErr error
	if s.cfg.TelegramBotToken == "" || settings.TelegramChatID == "" {
		sendErr = fmt.Errorf("Telegram 未配置或尚未绑定")
	} else {
		ctx, cancel := context.WithTimeout(r.Context(), 25*time.Second)
		sendErr = notify.NewTelegramBot(s.cfg.TelegramBotToken).SendTest(ctx, settings.TelegramChatID)
		cancel()
	}
	s.recordTest(w, r, "telegram", attempted, sendErr)
}

func (s *Server) recordTest(w http.ResponseWriter, r *http.Request, channel string, attempted time.Time, sendErr error) {
	result, message := "test_sent", ""
	if sendErr != nil {
		result, message = "test_failed", sendErr.Error()
	}
	if err := s.store.RecordTestNotification(r.Context(), channel, result, message, attempted); err != nil {
		s.serverError(w, r, err)
		return
	}
	status := "success"
	if sendErr != nil {
		status = "failed"
	}
	httpRedirect(w, r, "/notifications?test="+channel+"&result="+status)
}

func (s *Server) notificationHistory(w http.ResponseWriter, r *http.Request) {
	items, err := s.store.ListNotificationLogs(r.Context(), 100)
	if err != nil {
		s.serverError(w, r, err)
		return
	}
	settings, _, err := s.store.GetNotificationSettings(r.Context())
	if err != nil {
		s.serverError(w, r, err)
		return
	}
	zone, err := time.LoadLocation(settings.Timezone)
	if err != nil {
		zone = time.UTC
	}
	data := notificationsPageData{Title: "通知记录", CSRF: sessionFromContext(r.Context()).CSRF}
	if r.URL.Query().Get("result") == "success" {
		data.Success = "测试通知已发送。"
	} else if r.URL.Query().Get("result") == "failed" {
		data.Error = "测试通知发送失败，错误详情已记录在下方。"
	} else if r.URL.Query().Get("retried") == "1" {
		data.Success = "失败任务已重新加入队列。"
	}
	for _, item := range items {
		row := notificationLogRow{Item: item, Scheduled: item.ScheduledAt.In(zone).Format("2006-01-02 15:04:05"), Attempted: item.AttemptedAt.In(zone).Format("2006-01-02 15:04:05")}
		if item.Channel == "email" {
			row.ChannelLabel = "Email"
		} else {
			row.ChannelLabel = "Telegram"
		}
		switch item.Result {
		case "sent":
			row.ResultLabel = "已发送"
		case "retrying":
			row.ResultLabel = "等待重试"
		case "failed":
			row.ResultLabel = "失败"
		case "test_sent":
			row.ResultLabel = "测试成功"
		case "test_failed":
			row.ResultLabel = "测试失败"
		}
		if item.ReminderOffset >= 0 {
			row.OffsetLabel = fmt.Sprintf("提前 %d 天", item.ReminderOffset)
		} else {
			row.OffsetLabel = fmt.Sprintf("过期后 %d 天", -item.ReminderOffset)
		}
		row.Retryable = item.Result == "failed" && item.JobID.Valid
		data.Items = append(data.Items, row)
	}
	s.render(w, "notifications.html", data)
}

func (s *Server) retryNotification(w http.ResponseWriter, r *http.Request) {
	id, ok := parseID(w, r)
	if !ok {
		return
	}
	if err := s.store.RetryNotificationJob(r.Context(), id); err != nil {
		s.handleStoreError(w, r, err)
		return
	}
	httpRedirect(w, r, "/notifications?retried=1")
}

func (s *Server) settingsData(r *http.Request, settings store.NotificationSettings, rules store.ReminderRules) settingsPageData {
	return settingsPageData{
		Title: "提醒设置", CSRF: sessionFromContext(r.Context()).CSRF,
		Settings: settings, DefaultRules: joinOffsets(rules.Default), ImportantRules: joinOffsets(rules.Important),
		EmailConfigured: s.smtpConfig().Configured(), TelegramConfigured: s.cfg.TelegramBotToken != "",
		BotUsername: s.cfg.TelegramBotUsername,
	}
}

func joinOffsets(values []int) string {
	parts := make([]string, len(values))
	for i, value := range values {
		parts[i] = strconv.Itoa(value)
	}
	return strings.Join(parts, ", ")
}

func (s *Server) renderSettings(w http.ResponseWriter, data settingsPageData, status int) {
	tmpl, err := template.ParseFS(assets, "templates/layout.html", "templates/settings.html")
	if err != nil {
		s.serverError(w, nil, err)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	if err := tmpl.ExecuteTemplate(w, "layout.html", data); err != nil {
		s.logger.Error("render page", "page", "settings.html", "error", err)
	}
}

func (s *Server) smtpConfig() notify.SMTPConfig {
	return notify.SMTPConfig{
		Host: s.cfg.SMTPHost, Port: s.cfg.SMTPPort, Username: s.cfg.SMTPUsername,
		Password: s.cfg.SMTPPassword, From: s.cfg.SMTPFrom, Encryption: s.cfg.SMTPEncryption,
	}
}

func httpRedirect(w http.ResponseWriter, r *http.Request, path string) {
	http.Redirect(w, r, path, 303)
}

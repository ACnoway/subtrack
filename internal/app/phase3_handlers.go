package app

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/ACnoway/subtrack/internal/store"
)

type dashboardData struct {
	Title         string
	CSRF          string
	Total         int
	DueIn7Days    int
	DueIn30Days   int
	Expired       int
	CurrencyCosts []currencyCost
	CategoryCosts []categoryCost
	Upcoming      []subscriptionRow
}

type currencyCost struct {
	Currency string
	Monthly  float64
	Yearly   float64
}

type categoryCost struct {
	Label    string
	Currency string
	Yearly   float64
}

func (s *Server) dashboard(w http.ResponseWriter, r *http.Request) {
	items, err := s.store.ListAllSubscriptions(r.Context())
	if err != nil {
		s.serverError(w, r, err)
		return
	}
	today := dateOnly(time.Now().In(s.zone))
	costs := make(map[string]float64)
	categoryCosts := make(map[string]float64)
	data := dashboardData{Title: "仪表盘", CSRF: sessionFromContext(r.Context()).CSRF}
	for _, item := range items {
		if item.Archived {
			continue
		}
		data.Total++
		days := dateDifference(item.ExpiresAt, today)
		switch {
		case days < 0:
			data.Expired++
		case days <= 7:
			data.DueIn7Days++
			data.DueIn30Days++
		case days <= 30:
			data.DueIn30Days++
		}
		row := s.row(item)
		if (days < 0 || days <= 30) && len(data.Upcoming) < 8 {
			data.Upcoming = append(data.Upcoming, row)
		}
		if !item.RenewalPrice.Valid {
			continue
		}
		annual, ok := annualizedAmount(item.RenewalPrice.Float64, item.RenewalCycle, item.CustomCycleDays)
		if !ok {
			continue
		}
		currency := strings.TrimSpace(item.Currency)
		costs[currency] += annual
		categoryKey := item.Category + "\x00" + currency
		categoryCosts[categoryKey] += annual
	}
	for currency, yearly := range costs {
		data.CurrencyCosts = append(data.CurrencyCosts, currencyCost{Currency: currency, Monthly: yearly / 12, Yearly: yearly})
	}
	sort.Slice(data.CurrencyCosts, func(i, j int) bool { return data.CurrencyCosts[i].Currency < data.CurrencyCosts[j].Currency })
	for key, yearly := range categoryCosts {
		parts := strings.SplitN(key, "\x00", 2)
		data.CategoryCosts = append(data.CategoryCosts, categoryCost{Label: labelFor(categories, parts[0]), Currency: parts[1], Yearly: yearly})
	}
	sort.Slice(data.CategoryCosts, func(i, j int) bool {
		if data.CategoryCosts[i].Currency == data.CategoryCosts[j].Currency {
			return data.CategoryCosts[i].Yearly > data.CategoryCosts[j].Yearly
		}
		return data.CategoryCosts[i].Currency < data.CategoryCosts[j].Currency
	})
	s.render(w, "dashboard.html", data)
}

func annualizedAmount(price float64, cycle string, customDays int) (float64, bool) {
	factor := 0.0
	switch cycle {
	case "monthly":
		factor = 12
	case "quarterly":
		factor = 4
	case "semiannual":
		factor = 2
	case "annual":
		factor = 1
	case "custom":
		if customDays < 1 {
			return 0, false
		}
		factor = 365 / float64(customDays)
	default:
		return 0, false
	}
	return price * factor, true
}

func (s *Server) calendar(w http.ResponseWriter, r *http.Request) {
	month := dateOnly(time.Now().In(s.zone))
	month = time.Date(month.Year(), month.Month(), 1, 0, 0, 0, 0, s.zone)
	if raw := r.URL.Query().Get("month"); raw != "" {
		parsed, err := time.ParseInLocation("2006-01", raw, s.zone)
		if err != nil {
			http.Error(w, "月份格式无效，请使用 YYYY-MM。", http.StatusBadRequest)
			return
		}
		month = time.Date(parsed.Year(), parsed.Month(), 1, 0, 0, 0, 0, s.zone)
	}
	first := month
	last := first.AddDate(0, 1, -1)
	gridStart := first.AddDate(0, 0, -((int(first.Weekday()) + 6) % 7))
	gridEnd := last.AddDate(0, 0, (7-(int(last.Weekday())+6)%7)%7)
	items, err := s.store.ListSubscriptionsBetween(r.Context(), gridStart, gridEnd)
	if err != nil {
		s.serverError(w, r, err)
		return
	}
	byDate := make(map[string][]calendarEntry)
	for _, item := range items {
		expires := item.ExpiresAt.Format("2006-01-02")
		byDate[expires] = append(byDate[expires], calendarEntry{ID: item.ID, Label: "到期 · " + item.Name, Category: labelFor(categories, item.Category)})
		if item.NextBillingAt.Valid && item.NextBillingAt.Time.Format("2006-01-02") != expires {
			billing := item.NextBillingAt.Time.Format("2006-01-02")
			byDate[billing] = append(byDate[billing], calendarEntry{ID: item.ID, Label: "扣费 · " + item.Name, Category: labelFor(categories, item.Category), IsBilling: true})
		}
	}
	today := dateOnly(time.Now().In(s.zone))
	data := calendarData{
		Title: "到期日历", CSRF: sessionFromContext(r.Context()).CSRF,
		MonthLabel: month.Format("2006 年 1 月"), CurrentMonth: month.Format("2006-01"),
		PreviousMonth: month.AddDate(0, -1, 0).Format("2006-01"), NextMonth: month.AddDate(0, 1, 0).Format("2006-01"),
	}
	week := make([]calendarDay, 0, 7)
	for day := gridStart; !day.After(gridEnd); day = day.AddDate(0, 0, 1) {
		week = append(week, calendarDay{
			Label: strconv.Itoa(day.Day()), Date: day.Format("2006-01-02"), InMonth: day.Month() == month.Month(),
			IsToday: sameDate(day, today), Entries: byDate[day.Format("2006-01-02")],
		})
		if len(week) == 7 {
			data.Weeks = append(data.Weeks, week)
			week = make([]calendarDay, 0, 7)
		}
	}
	s.render(w, "calendar.html", data)
}

type calendarData struct {
	Title         string
	CSRF          string
	MonthLabel    string
	CurrentMonth  string
	PreviousMonth string
	NextMonth     string
	Weeks         [][]calendarDay
}

type calendarDay struct {
	Label   string
	Date    string
	InMonth bool
	IsToday bool
	Entries []calendarEntry
}

type calendarEntry struct {
	ID        int64
	Label     string
	Category  string
	IsBilling bool
}

type renewalForm struct {
	RenewedAt     string
	NewExpiresAt  string
	NextBillingAt string
	Amount        string
	Currency      string
	Notes         string
}

type renewalPageData struct {
	Title           string
	CSRF            string
	Error           string
	Cycles          []option
	Item            store.Subscription
	Form            renewalForm
	PreviousExpires string
}

func (s *Server) newRenewal(w http.ResponseWriter, r *http.Request) {
	id, ok := parseID(w, r)
	if !ok {
		return
	}
	item, err := s.store.GetSubscription(r.Context(), id)
	if err != nil {
		s.handleStoreError(w, r, err)
		return
	}
	if item.Archived {
		http.Error(w, "已归档资产不能确认续费。", http.StatusConflict)
		return
	}
	data := s.renewalPage(r, item, defaultRenewalForm(item, time.Now().In(s.zone)), "")
	s.render(w, "renewal_form.html", data)
}

func (s *Server) createRenewal(w http.ResponseWriter, r *http.Request) {
	id, ok := parseID(w, r)
	if !ok {
		return
	}
	item, err := s.store.GetSubscription(r.Context(), id)
	if err != nil {
		s.handleStoreError(w, r, err)
		return
	}
	form, renewedAt, newExpiry, nextBilling, amount, currency, err := parseRenewalForm(r, item)
	if err != nil {
		data := s.renewalPage(r, item, form, err.Error())
		s.renderStatus(w, "renewal_form.html", data, http.StatusUnprocessableEntity)
		return
	}
	err = s.store.RecordRenewal(r.Context(), id, renewedAt, newExpiry, nextBilling, amount, currency, form.Notes)
	if errors.Is(err, store.ErrNotFound) {
		http.NotFound(w, r)
		return
	}
	if errors.Is(err, store.ErrArchivedSubscription) {
		http.Error(w, "已归档资产不能确认续费。", http.StatusConflict)
		return
	}
	if err != nil {
		s.serverError(w, r, err)
		return
	}
	http.Redirect(w, r, "/renewals?success=1", http.StatusSeeOther)
}

func (s *Server) renewalPage(r *http.Request, item store.Subscription, form renewalForm, message string) renewalPageData {
	return renewalPageData{
		Title: "确认续费", CSRF: sessionFromContext(r.Context()).CSRF, Error: message, Cycles: renewalCycles,
		Item: item, Form: form, PreviousExpires: item.ExpiresAt.Format("2006-01-02"),
	}
}

func defaultRenewalForm(item store.Subscription, now time.Time) renewalForm {
	today := dateOnly(now)
	base := dateOnly(item.ExpiresAt)
	if base.Before(today) {
		base = today
	}
	form := renewalForm{RenewedAt: today.Format("2006-01-02"), Currency: strings.TrimSpace(item.Currency)}
	if item.RenewalPrice.Valid {
		form.Amount = strconv.FormatFloat(item.RenewalPrice.Float64, 'f', 2, 64)
	}
	if next, ok := nextCycleDate(base, item.RenewalCycle, item.CustomCycleDays); ok {
		form.NewExpiresAt = next.Format("2006-01-02")
	}
	if item.NextBillingAt.Valid {
		billingBase := dateOnly(item.NextBillingAt.Time)
		if billingBase.Before(today) {
			billingBase = today
		}
		if next, ok := nextCycleDate(billingBase, item.RenewalCycle, item.CustomCycleDays); ok {
			form.NextBillingAt = next.Format("2006-01-02")
		}
	}
	if form.NextBillingAt == "" {
		form.NextBillingAt = form.NewExpiresAt
	}
	return form
}

func parseRenewalForm(r *http.Request, item store.Subscription) (renewalForm, time.Time, time.Time, sql.NullTime, sql.NullFloat64, string, error) {
	_ = r.ParseForm()
	form := renewalForm{
		RenewedAt:     strings.TrimSpace(r.FormValue("renewed_at")),
		NewExpiresAt:  strings.TrimSpace(r.FormValue("new_expires_at")),
		NextBillingAt: strings.TrimSpace(r.FormValue("next_billing_at")),
		Amount:        strings.TrimSpace(r.FormValue("amount")),
		Currency:      strings.ToUpper(strings.TrimSpace(r.FormValue("currency"))),
		Notes:         strings.TrimSpace(r.FormValue("notes")),
	}
	renewed, err := parseDate(form.RenewedAt, true)
	if err != nil {
		return form, time.Time{}, time.Time{}, sql.NullTime{}, sql.NullFloat64{}, "", fmt.Errorf("续费日期无效。")
	}
	expires, err := parseDate(form.NewExpiresAt, true)
	if err != nil || !expires.Time.After(renewed.Time) {
		return form, time.Time{}, time.Time{}, sql.NullTime{}, sql.NullFloat64{}, "", fmt.Errorf("新的到期日期必须晚于续费日期。")
	}
	nextBilling, err := parseDate(form.NextBillingAt, false)
	if err != nil {
		return form, time.Time{}, time.Time{}, sql.NullTime{}, sql.NullFloat64{}, "", fmt.Errorf("下次扣费日期无效。")
	}
	if len(form.Currency) != 3 || strings.Trim(form.Currency, "ABCDEFGHIJKLMNOPQRSTUVWXYZ") != "" {
		return form, time.Time{}, time.Time{}, sql.NullTime{}, sql.NullFloat64{}, "", fmt.Errorf("货币请填写 3 位大写代码，例如 USD 或 CNY。")
	}
	if utf8.RuneCountInString(form.Notes) > 1000 {
		return form, time.Time{}, time.Time{}, sql.NullTime{}, sql.NullFloat64{}, "", fmt.Errorf("续费备注不能超过 1000 个字符。")
	}
	var amount sql.NullFloat64
	if form.Amount != "" {
		value, err := strconv.ParseFloat(form.Amount, 64)
		if err != nil || math.IsNaN(value) || math.IsInf(value, 0) || value < 0 || value >= 10000000000 {
			return form, time.Time{}, time.Time{}, sql.NullTime{}, sql.NullFloat64{}, "", fmt.Errorf("实付金额请填写 0 到 9,999,999,999.99 之间的数字。")
		}
		amount = sql.NullFloat64{Float64: value, Valid: true}
	}
	return form, renewed.Time, expires.Time, nextBilling, amount, form.Currency, nil
}

type renewalHistoryRow struct {
	store.RenewalRecord
	RenewedLabel  string
	PreviousLabel string
	NewLabel      string
	AmountLabel   string
}

type renewalsData struct {
	Title   string
	CSRF    string
	Success bool
	Items   []renewalHistoryRow
}

func (s *Server) renewalHistory(w http.ResponseWriter, r *http.Request) {
	items, err := s.store.ListRenewals(r.Context(), 5000)
	if err != nil {
		s.serverError(w, r, err)
		return
	}
	data := renewalsData{Title: "续费历史", CSRF: sessionFromContext(r.Context()).CSRF, Success: r.URL.Query().Get("success") == "1"}
	for _, item := range items {
		amount := "—"
		if item.Amount.Valid {
			amount = fmt.Sprintf("%s %.2f", strings.TrimSpace(item.Currency), item.Amount.Float64)
		}
		data.Items = append(data.Items, renewalHistoryRow{
			RenewalRecord: item, RenewedLabel: item.RenewedAt.Format("2006-01-02"),
			PreviousLabel: item.PreviousExpiresAt.Format("2006-01-02"), NewLabel: item.NewExpiresAt.Format("2006-01-02"), AmountLabel: amount,
		})
	}
	s.render(w, "renewals.html", data)
}

type backupEnvelope struct {
	Format        string               `json:"format"`
	Version       int                  `json:"version"`
	ExportedAt    time.Time            `json:"exported_at"`
	Subscriptions []backupSubscription `json:"subscriptions"`
	Renewals      []backupRenewal      `json:"renewals"`
}

type backupSubscription struct {
	ReferenceID     int64    `json:"reference_id"`
	Name            string   `json:"name"`
	Category        string   `json:"category"`
	Provider        string   `json:"provider"`
	AccountLabel    string   `json:"account_label"`
	Description     string   `json:"description"`
	StartDate       string   `json:"start_date"`
	ExpiresAt       string   `json:"expires_at"`
	NextBillingAt   string   `json:"next_billing_at"`
	RenewalCycle    string   `json:"renewal_cycle"`
	CustomCycleDays int      `json:"custom_cycle_days,omitempty"`
	RenewalPrice    *float64 `json:"renewal_price"`
	Currency        string   `json:"currency"`
	AutoRenew       bool     `json:"auto_renew"`
	RenewalURL      string   `json:"renewal_url"`
	Priority        int      `json:"priority"`
	Tags            string   `json:"tags"`
	Notes           string   `json:"notes"`
	Archived        bool     `json:"archived"`
}

type backupRenewal struct {
	SubscriptionRef   int64     `json:"subscription_ref,omitempty"`
	SubscriptionName  string    `json:"subscription_name"`
	RenewedAt         string    `json:"renewed_at"`
	PreviousExpiresAt string    `json:"previous_expires_at"`
	NewExpiresAt      string    `json:"new_expires_at"`
	Amount            *float64  `json:"amount"`
	Currency          string    `json:"currency"`
	Notes             string    `json:"notes"`
	CreatedAt         time.Time `json:"created_at"`
}

type dataPageData struct {
	Title             string
	CSRF              string
	Error             string
	Success           string
	SubscriptionCount int
	RenewalCount      int
}

func (s *Server) dataPage(w http.ResponseWriter, r *http.Request) {
	subs, err := s.store.ListAllSubscriptions(r.Context())
	if err != nil {
		s.serverError(w, r, err)
		return
	}
	renewals, err := s.store.ListRenewals(r.Context(), 5000)
	if err != nil {
		s.serverError(w, r, err)
		return
	}
	data := dataPageData{Title: "数据备份", CSRF: sessionFromContext(r.Context()).CSRF, SubscriptionCount: len(subs), RenewalCount: len(renewals)}
	if r.URL.Query().Get("imported") == "1" {
		data.Success = "备份数据已导入。"
	}
	s.render(w, "data.html", data)
}

func (s *Server) exportData(w http.ResponseWriter, r *http.Request) {
	subs, err := s.store.ListAllSubscriptions(r.Context())
	if err != nil {
		s.serverError(w, r, err)
		return
	}
	renewals, err := s.store.ListRenewals(r.Context(), 5001)
	if err != nil {
		s.serverError(w, r, err)
		return
	}
	if len(subs) > 5000 || len(renewals) > 5000 {
		http.Error(w, "完整备份暂支持最多 5000 项资产和 5000 条续费记录，请先拆分或清理数据。", http.StatusRequestEntityTooLarge)
		return
	}
	bundle := backupEnvelope{Format: "subtrack-backup", Version: 1, ExportedAt: time.Now().UTC()}
	for _, item := range subs {
		var price *float64
		if item.RenewalPrice.Valid {
			value := item.RenewalPrice.Float64
			price = &value
		}
		bundle.Subscriptions = append(bundle.Subscriptions, backupSubscription{
			ReferenceID: item.ID, Name: item.Name, Category: item.Category, Provider: item.Provider,
			AccountLabel: item.AccountLabel, Description: item.Description, StartDate: nullableDateLabel(item.StartDate),
			ExpiresAt: item.ExpiresAt.Format("2006-01-02"), NextBillingAt: nullableDateLabel(item.NextBillingAt),
			RenewalCycle: item.RenewalCycle, CustomCycleDays: item.CustomCycleDays, RenewalPrice: price,
			Currency: strings.TrimSpace(item.Currency), AutoRenew: item.AutoRenew, RenewalURL: item.RenewalURL,
			Priority: item.Priority, Tags: item.Tags, Notes: item.Notes, Archived: item.Archived,
		})
	}
	for _, item := range renewals {
		var amount *float64
		if item.Amount.Valid {
			value := item.Amount.Float64
			amount = &value
		}
		record := backupRenewal{
			SubscriptionName: item.SubscriptionName, RenewedAt: item.RenewedAt.Format("2006-01-02"),
			PreviousExpiresAt: item.PreviousExpiresAt.Format("2006-01-02"), NewExpiresAt: item.NewExpiresAt.Format("2006-01-02"),
			Amount: amount, Currency: strings.TrimSpace(item.Currency), Notes: item.Notes, CreatedAt: item.CreatedAt,
		}
		if item.SubscriptionID.Valid {
			record.SubscriptionRef = item.SubscriptionID.Int64
		}
		bundle.Renewals = append(bundle.Renewals, record)
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Content-Disposition", `attachment; filename="subtrack-backup.json"`)
	if err := json.NewEncoder(w).Encode(bundle); err != nil {
		s.logger.Error("write data export", "error", err)
	}
}

func nullableDateLabel(value sql.NullTime) string {
	if !value.Valid {
		return ""
	}
	return value.Time.Format("2006-01-02")
}

func (s *Server) importData(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseMultipartForm(1 << 20); err != nil {
		s.renderDataError(w, r, "备份文件无法读取，文件大小不能超过 4 MiB。", http.StatusBadRequest)
		return
	}
	file, _, err := r.FormFile("backup")
	if err != nil {
		s.renderDataError(w, r, "请选择要导入的 JSON 备份文件。", http.StatusBadRequest)
		return
	}
	defer file.Close()
	contents, err := io.ReadAll(io.LimitReader(file, (4<<20)+1))
	if err != nil || len(contents) > 4<<20 {
		s.renderDataError(w, r, "备份文件无法读取，文件大小不能超过 4 MiB。", http.StatusBadRequest)
		return
	}
	bundle, err := parseBackup(contents)
	if err != nil {
		s.renderDataError(w, r, err.Error(), http.StatusUnprocessableEntity)
		return
	}
	if err := s.store.ImportBundle(r.Context(), bundle); err != nil {
		s.serverError(w, r, err)
		return
	}
	http.Redirect(w, r, "/data?imported=1", http.StatusSeeOther)
}

func (s *Server) renderDataError(w http.ResponseWriter, r *http.Request, message string, status int) {
	subs, subErr := s.store.ListAllSubscriptions(r.Context())
	renewals, renewalErr := s.store.ListRenewals(r.Context(), 5000)
	if subErr != nil || renewalErr != nil {
		s.serverError(w, r, errors.Join(subErr, renewalErr))
		return
	}
	s.renderStatus(w, "data.html", dataPageData{
		Title: "数据备份", CSRF: sessionFromContext(r.Context()).CSRF, Error: message,
		SubscriptionCount: len(subs), RenewalCount: len(renewals),
	}, status)
}

func parseBackup(contents []byte) (store.ImportBundle, error) {
	decoder := json.NewDecoder(strings.NewReader(string(contents)))
	decoder.DisallowUnknownFields()
	var input backupEnvelope
	if err := decoder.Decode(&input); err != nil {
		return store.ImportBundle{}, fmt.Errorf("JSON 备份格式无效：%w", err)
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		return store.ImportBundle{}, fmt.Errorf("备份文件只能包含一个 JSON 对象。")
	}
	if input.Format != "subtrack-backup" || input.Version != 1 {
		return store.ImportBundle{}, fmt.Errorf("不支持的备份格式或版本。")
	}
	if len(input.Subscriptions) > 5000 || len(input.Renewals) > 5000 {
		return store.ImportBundle{}, fmt.Errorf("单个备份最多可包含 5000 条资产和 5000 条续费记录。")
	}
	if len(input.Subscriptions) == 0 && len(input.Renewals) == 0 {
		return store.ImportBundle{}, fmt.Errorf("备份中没有可导入的数据。")
	}
	result := store.ImportBundle{}
	references := make(map[int64]struct{}, len(input.Subscriptions))
	for index, backup := range input.Subscriptions {
		if backup.ReferenceID <= 0 {
			return store.ImportBundle{}, fmt.Errorf("第 %d 条资产缺少有效的 reference_id。", index+1)
		}
		if _, exists := references[backup.ReferenceID]; exists {
			return store.ImportBundle{}, fmt.Errorf("备份包含重复的资产 reference_id %d。", backup.ReferenceID)
		}
		references[backup.ReferenceID] = struct{}{}
		start := backup.StartDate
		nextBilling := backup.NextBillingAt
		price := ""
		if backup.RenewalPrice != nil {
			price = strconv.FormatFloat(*backup.RenewalPrice, 'f', -1, 64)
		}
		form := subscriptionForm{
			Name: backup.Name, Category: backup.Category, Provider: backup.Provider, AccountLabel: backup.AccountLabel,
			Description: backup.Description, StartDate: start, ExpiresAt: backup.ExpiresAt,
			NextBillingAt: nextBilling, RenewalCycle: backup.RenewalCycle, CustomCycleDays: backup.CustomCycleDays,
			RenewalPrice: price, Currency: strings.ToUpper(backup.Currency), AutoRenew: backup.AutoRenew,
			RenewalURL: backup.RenewalURL, Priority: backup.Priority, Tags: backup.Tags, Notes: backup.Notes,
		}
		_, item, err := validateSubscriptionForm(form)
		if err != nil {
			return store.ImportBundle{}, fmt.Errorf("第 %d 条资产无效：%s", index+1, err)
		}
		item.Archived = backup.Archived
		result.Subscriptions = append(result.Subscriptions, store.ImportedSubscription{ReferenceID: backup.ReferenceID, Item: item})
	}
	for index, backup := range input.Renewals {
		renewedAt, err := time.Parse("2006-01-02", backup.RenewedAt)
		if err != nil {
			return store.ImportBundle{}, fmt.Errorf("第 %d 条续费记录的续费日期无效。", index+1)
		}
		previous, err := time.Parse("2006-01-02", backup.PreviousExpiresAt)
		if err != nil {
			return store.ImportBundle{}, fmt.Errorf("第 %d 条续费记录的原到期日期无效。", index+1)
		}
		next, err := time.Parse("2006-01-02", backup.NewExpiresAt)
		if err != nil || !next.After(renewedAt) {
			return store.ImportBundle{}, fmt.Errorf("第 %d 条续费记录的新到期日期必须晚于续费日期。", index+1)
		}
		currency := strings.ToUpper(backup.Currency)
		if backup.SubscriptionRef < 0 || !validCurrency(currency) || utf8.RuneCountInString(backup.SubscriptionName) == 0 || utf8.RuneCountInString(backup.SubscriptionName) > 120 || utf8.RuneCountInString(backup.Notes) > 1000 {
			return store.ImportBundle{}, fmt.Errorf("第 %d 条续费记录包含无效字段。", index+1)
		}
		var amount sql.NullFloat64
		if backup.Amount != nil {
			if math.IsNaN(*backup.Amount) || math.IsInf(*backup.Amount, 0) || *backup.Amount < 0 || *backup.Amount >= 10000000000 {
				return store.ImportBundle{}, fmt.Errorf("第 %d 条续费记录的实付金额无效。", index+1)
			}
			amount = sql.NullFloat64{Float64: *backup.Amount, Valid: true}
		}
		createdAt := backup.CreatedAt
		if createdAt.IsZero() {
			createdAt = time.Now().UTC()
		}
		result.Renewals = append(result.Renewals, store.RenewalRecord{
			SubscriptionRef: backup.SubscriptionRef, SubscriptionName: backup.SubscriptionName,
			RenewedAt: renewedAt, PreviousExpiresAt: previous, NewExpiresAt: next,
			Amount: amount, Currency: currency, Notes: backup.Notes, CreatedAt: createdAt,
		})
	}
	return result, nil
}

func validCurrency(value string) bool {
	return len(value) == 3 && strings.Trim(value, "ABCDEFGHIJKLMNOPQRSTUVWXYZ") == ""
}

func dateOnly(value time.Time) time.Time {
	return time.Date(value.Year(), value.Month(), value.Day(), 0, 0, 0, 0, value.Location())
}

func dateDifference(later, earlier time.Time) int {
	a := time.Date(later.Year(), later.Month(), later.Day(), 0, 0, 0, 0, time.UTC)
	b := time.Date(earlier.Year(), earlier.Month(), earlier.Day(), 0, 0, 0, 0, time.UTC)
	return int(a.Sub(b).Hours() / 24)
}

func sameDate(left, right time.Time) bool {
	return left.Year() == right.Year() && left.Month() == right.Month() && left.Day() == right.Day()
}

func nextCycleDate(base time.Time, cycle string, customDays int) (time.Time, bool) {
	base = dateOnly(base)
	switch cycle {
	case "monthly":
		return addMonthsClamped(base, 1), true
	case "quarterly":
		return addMonthsClamped(base, 3), true
	case "semiannual":
		return addMonthsClamped(base, 6), true
	case "annual":
		return addMonthsClamped(base, 12), true
	case "custom":
		if customDays > 0 {
			return base.AddDate(0, 0, customDays), true
		}
	}
	return time.Time{}, false
}

func addMonthsClamped(base time.Time, months int) time.Time {
	first := time.Date(base.Year(), base.Month()+time.Month(months), 1, 0, 0, 0, 0, base.Location())
	lastDay := first.AddDate(0, 1, -1).Day()
	day := min(base.Day(), lastDay)
	return time.Date(first.Year(), first.Month(), day, 0, 0, 0, 0, base.Location())
}

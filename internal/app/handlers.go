package app

import (
	"crypto/sha256"
	"crypto/subtle"
	"database/sql"
	"errors"
	"fmt"
	"html/template"
	"math"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/ACnoway/subtrack/internal/store"
)

var categories = []option{
	{Value: "server", Label: "VPS / 服务器"},
	{Value: "domain", Label: "域名"},
	{Value: "cloud", Label: "云服务"},
	{Value: "software", Label: "软件订阅"},
	{Value: "api", Label: "API 服务"},
	{Value: "certificate", Label: "SSL 证书"},
	{Value: "other", Label: "其他"},
}

var renewalCycles = []option{
	{Value: "monthly", Label: "月付"},
	{Value: "quarterly", Label: "季付"},
	{Value: "semiannual", Label: "半年付"},
	{Value: "annual", Label: "年付"},
	{Value: "custom", Label: "自定义"},
	{Value: "one_time", Label: "一次性"},
}

type option struct {
	Value string
	Label string
}

type subscriptionForm struct {
	ID            int64
	Name          string
	Category      string
	Provider      string
	AccountLabel  string
	Description   string
	StartDate     string
	ExpiresAt     string
	NextBillingAt string
	RenewalCycle  string
	RenewalPrice  string
	Currency      string
	AutoRenew     bool
	RenewalURL    string
	Priority      int
	Tags          string
	Notes         string
}

type subscriptionRow struct {
	Item          store.Subscription
	CategoryLabel string
	CycleLabel    string
	ExpiryLabel   string
	DaysRemaining int
	DaysOverdue   int
	StatusLabel   string
	StatusClass   string
	PriceLabel    string
}

type pageData struct {
	Title      string
	CSRF       string
	Error      string
	Search     string
	Status     string
	Category   string
	Categories []option
	Cycles     []option
	Items      []subscriptionRow
	Form       subscriptionForm
	IsEdit     bool
	Now        time.Time
}

func (s *Server) home(w http.ResponseWriter, r *http.Request) {
	http.Redirect(w, r, "/subscriptions", http.StatusSeeOther)
}

func (s *Server) loginPage(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.readSession(r); ok {
		http.Redirect(w, r, "/subscriptions", http.StatusSeeOther)
		return
	}
	token, err := s.setLoginCSRF(w)
	if err != nil {
		s.serverError(w, r, err)
		return
	}
	data := pageData{Title: "登录", CSRF: token}
	if r.URL.Query().Get("error") == "credentials" {
		data.Error = "用户名或密码不正确。"
	}
	s.render(w, "login.html", data)
}

func (s *Server) login(w http.ResponseWriter, r *http.Request) {
	cookie, err := r.Cookie(csrfCookie)
	if err != nil || cookie.Value == "" || cookie.Value != r.FormValue("csrf_token") {
		http.Error(w, "CSRF token 校验失败，请刷新登录页面后重试。", http.StatusForbidden)
		return
	}
	usernameOK := constantTimeEqual(r.FormValue("username"), s.cfg.AdminUsername)
	passwordOK := constantTimeEqual(r.FormValue("password"), s.cfg.AdminPassword)
	if !usernameOK || !passwordOK {
		http.Redirect(w, r, "/login?error=credentials", http.StatusSeeOther)
		return
	}
	token, err := randomToken()
	if err != nil {
		s.serverError(w, r, err)
		return
	}
	http.SetCookie(w, &http.Cookie{Name: csrfCookie, Value: "", Path: "/", HttpOnly: true, Secure: s.cfg.CookieSecure, SameSite: http.SameSiteStrictMode, MaxAge: -1})
	s.setSession(w, token)
	http.Redirect(w, r, "/subscriptions", http.StatusSeeOther)
}

func (s *Server) logout(w http.ResponseWriter, r *http.Request) {
	s.clearSession(w)
	http.Redirect(w, r, "/login", http.StatusSeeOther)
}

func (s *Server) listSubscriptions(w http.ResponseWriter, r *http.Request) {
	status := r.URL.Query().Get("status")
	switch status {
	case "all", "upcoming", "expired", "active", "archived":
	default:
		status = "all"
	}
	category := r.URL.Query().Get("category")
	if category == "all" || !validCategory(category) {
		category = ""
	}
	search := strings.TrimSpace(r.URL.Query().Get("q"))
	items, err := s.store.ListSubscriptions(r.Context(), search, status, category)
	if err != nil {
		s.serverError(w, r, err)
		return
	}
	data := s.pageData(r, "订阅与资产", "")
	data.Search = search
	data.Status = status
	data.Category = category
	for _, item := range items {
		data.Items = append(data.Items, s.row(item))
	}
	s.render(w, "subscriptions.html", data)
}

func (s *Server) newSubscription(w http.ResponseWriter, r *http.Request) {
	data := s.pageData(r, "添加资产", "")
	data.Form = subscriptionForm{Category: "server", RenewalCycle: "annual", Currency: "USD", Priority: 2}
	s.render(w, "form.html", data)
}

func (s *Server) createSubscription(w http.ResponseWriter, r *http.Request) {
	form, item, err := parseSubscriptionForm(r)
	if err != nil {
		data := s.pageData(r, "添加资产", err.Error())
		data.Form = form
		s.renderStatus(w, "form.html", data, http.StatusUnprocessableEntity)
		return
	}
	if _, err := s.store.SaveSubscription(r.Context(), item); err != nil {
		s.serverError(w, r, err)
		return
	}
	http.Redirect(w, r, "/subscriptions", http.StatusSeeOther)
}

func (s *Server) editSubscription(w http.ResponseWriter, r *http.Request) {
	id, ok := parseID(w, r)
	if !ok {
		return
	}
	item, err := s.store.GetSubscription(r.Context(), id)
	if err != nil {
		s.handleStoreError(w, r, err)
		return
	}
	data := s.pageData(r, "编辑资产", "")
	data.IsEdit = true
	data.Form = formFromItem(item)
	s.render(w, "form.html", data)
}

func (s *Server) updateSubscription(w http.ResponseWriter, r *http.Request) {
	id, ok := parseID(w, r)
	if !ok {
		return
	}
	form, item, err := parseSubscriptionForm(r)
	form.ID, item.ID = id, id
	if err != nil {
		data := s.pageData(r, "编辑资产", err.Error())
		data.IsEdit = true
		data.Form = form
		s.renderStatus(w, "form.html", data, http.StatusUnprocessableEntity)
		return
	}
	if _, err := s.store.SaveSubscription(r.Context(), item); err != nil {
		s.handleStoreError(w, r, err)
		return
	}
	http.Redirect(w, r, "/subscriptions", http.StatusSeeOther)
}

func (s *Server) archiveSubscription(w http.ResponseWriter, r *http.Request) {
	s.changeArchiveStatus(w, r, true)
}

func (s *Server) restoreSubscription(w http.ResponseWriter, r *http.Request) {
	s.changeArchiveStatus(w, r, false)
}

func (s *Server) changeArchiveStatus(w http.ResponseWriter, r *http.Request, archived bool) {
	id, ok := parseID(w, r)
	if !ok {
		return
	}
	if err := s.store.SetArchived(r.Context(), id, archived); err != nil {
		s.handleStoreError(w, r, err)
		return
	}
	http.Redirect(w, r, "/subscriptions", http.StatusSeeOther)
}

func (s *Server) deleteSubscription(w http.ResponseWriter, r *http.Request) {
	id, ok := parseID(w, r)
	if !ok {
		return
	}
	if err := s.store.DeleteSubscription(r.Context(), id); err != nil {
		s.handleStoreError(w, r, err)
		return
	}
	http.Redirect(w, r, "/subscriptions", http.StatusSeeOther)
}

func (s *Server) pageData(r *http.Request, title, message string) pageData {
	return pageData{Title: title, CSRF: sessionFromContext(r.Context()).CSRF, Error: message, Categories: categories, Cycles: renewalCycles, Now: time.Now()}
}

func (s *Server) render(w http.ResponseWriter, page string, data any) {
	s.renderStatus(w, page, data, http.StatusOK)
}

func (s *Server) renderStatus(w http.ResponseWriter, page string, data any, status int) {
	tmpl, err := template.ParseFS(assets, "templates/layout.html", "templates/"+page)
	if err != nil {
		s.serverError(w, nil, err)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	if err := tmpl.ExecuteTemplate(w, "layout.html", data); err != nil {
		s.logger.Error("render page", "page", page, "error", err)
	}
}

func (s *Server) serverError(w http.ResponseWriter, r *http.Request, err error) {
	if r != nil {
		s.logger.Error("request failed", "path", r.URL.Path, "error", err)
	} else {
		s.logger.Error("request failed", "error", err)
	}
	http.Error(w, "服务器内部错误", http.StatusInternalServerError)
}

func (s *Server) handleStoreError(w http.ResponseWriter, r *http.Request, err error) {
	if errors.Is(err, store.ErrNotFound) {
		http.NotFound(w, r)
		return
	}
	s.serverError(w, r, err)
}

func parseID(w http.ResponseWriter, r *http.Request) (int64, bool) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id < 1 {
		http.NotFound(w, r)
		return 0, false
	}
	return id, true
}

func parseSubscriptionForm(r *http.Request) (subscriptionForm, store.Subscription, error) {
	_ = r.ParseForm()
	form := subscriptionForm{
		Name: strings.TrimSpace(r.FormValue("name")), Category: r.FormValue("category"),
		Provider: strings.TrimSpace(r.FormValue("provider")), AccountLabel: strings.TrimSpace(r.FormValue("account_label")),
		Description: strings.TrimSpace(r.FormValue("description")), StartDate: r.FormValue("start_date"),
		ExpiresAt: r.FormValue("expires_at"), NextBillingAt: r.FormValue("next_billing_at"),
		RenewalCycle: r.FormValue("renewal_cycle"), RenewalPrice: strings.TrimSpace(r.FormValue("renewal_price")),
		Currency: strings.ToUpper(strings.TrimSpace(r.FormValue("currency"))), AutoRenew: r.FormValue("auto_renew") == "on",
		RenewalURL: strings.TrimSpace(r.FormValue("renewal_url")), Tags: strings.TrimSpace(r.FormValue("tags")),
		Notes: strings.TrimSpace(r.FormValue("notes")),
	}
	form.Priority, _ = strconv.Atoi(r.FormValue("priority"))
	if utf8.RuneCountInString(form.Name) == 0 || utf8.RuneCountInString(form.Name) > 120 {
		return form, store.Subscription{}, fmt.Errorf("资产名称不能为空，且不能超过 120 个字符。")
	}
	if utf8.RuneCountInString(form.Provider) > 200 || utf8.RuneCountInString(form.AccountLabel) > 200 ||
		utf8.RuneCountInString(form.Description) > 1000 || utf8.RuneCountInString(form.Tags) > 500 ||
		utf8.RuneCountInString(form.Notes) > 4000 || utf8.RuneCountInString(form.RenewalURL) > 2048 {
		return form, store.Subscription{}, fmt.Errorf("某些文本字段超过允许长度，请检查表单内容。")
	}
	if !validCategory(form.Category) {
		return form, store.Subscription{}, fmt.Errorf("请选择有效的资产类别。")
	}
	if !validCycle(form.RenewalCycle) {
		return form, store.Subscription{}, fmt.Errorf("请选择有效的续费周期。")
	}
	if len(form.Currency) != 3 || strings.Trim(form.Currency, "ABCDEFGHIJKLMNOPQRSTUVWXYZ") != "" {
		return form, store.Subscription{}, fmt.Errorf("货币请填写 3 位大写代码，例如 USD 或 CNY。")
	}
	if form.Priority < 1 || form.Priority > 3 {
		return form, store.Subscription{}, fmt.Errorf("优先级无效。")
	}
	expiresAt, err := parseDate(form.ExpiresAt, true)
	if err != nil {
		return form, store.Subscription{}, fmt.Errorf("到期日期无效，请填写完整日期。")
	}
	startDate, err := parseDate(form.StartDate, false)
	if err != nil {
		return form, store.Subscription{}, fmt.Errorf("购买日期无效。")
	}
	nextBillingAt, err := parseDate(form.NextBillingAt, false)
	if err != nil {
		return form, store.Subscription{}, fmt.Errorf("下次扣费日期无效。")
	}
	var price sql.NullFloat64
	if form.RenewalPrice != "" {
		value, err := strconv.ParseFloat(form.RenewalPrice, 64)
		if err != nil || math.IsNaN(value) || math.IsInf(value, 0) || value < 0 || value >= 10000000000 {
			return form, store.Subscription{}, fmt.Errorf("续费价格请填写 0 到 9,999,999,999.99 之间的数字。")
		}
		price = sql.NullFloat64{Float64: value, Valid: true}
	}
	if form.RenewalURL != "" {
		parsed, err := url.ParseRequestURI(form.RenewalURL)
		if err != nil || (parsed.Scheme != "https" && parsed.Scheme != "http") || parsed.Host == "" {
			return form, store.Subscription{}, fmt.Errorf("续费链接必须是完整的 HTTP 或 HTTPS URL。")
		}
	}
	item := store.Subscription{
		Name: form.Name, Category: form.Category, Provider: form.Provider, AccountLabel: form.AccountLabel,
		Description: form.Description, StartDate: startDate, ExpiresAt: expiresAt.Time, NextBillingAt: nextBillingAt,
		RenewalCycle: form.RenewalCycle, RenewalPrice: price, Currency: form.Currency,
		AutoRenew: form.AutoRenew, RenewalURL: form.RenewalURL, Priority: form.Priority,
		Tags: form.Tags, Notes: form.Notes,
	}
	return form, item, nil
}

func formFromItem(item store.Subscription) subscriptionForm {
	form := subscriptionForm{
		ID: item.ID, Name: item.Name, Category: item.Category, Provider: item.Provider,
		AccountLabel: item.AccountLabel, Description: item.Description, ExpiresAt: item.ExpiresAt.Format("2006-01-02"),
		RenewalCycle: item.RenewalCycle, Currency: strings.TrimSpace(item.Currency), AutoRenew: item.AutoRenew,
		RenewalURL: item.RenewalURL, Priority: item.Priority, Tags: item.Tags, Notes: item.Notes,
	}
	if item.StartDate.Valid {
		form.StartDate = item.StartDate.Time.Format("2006-01-02")
	}
	if item.NextBillingAt.Valid {
		form.NextBillingAt = item.NextBillingAt.Time.Format("2006-01-02")
	}
	if item.RenewalPrice.Valid {
		form.RenewalPrice = strconv.FormatFloat(item.RenewalPrice.Float64, 'f', 2, 64)
	}
	return form
}

func parseDate(value string, required bool) (sql.NullTime, error) {
	if strings.TrimSpace(value) == "" {
		if required {
			return sql.NullTime{}, errors.New("date is required")
		}
		return sql.NullTime{}, nil
	}
	parsed, err := time.Parse("2006-01-02", value)
	if err != nil {
		return sql.NullTime{}, err
	}
	return sql.NullTime{Time: parsed, Valid: true}, nil
}

func validCategory(value string) bool {
	for _, category := range categories {
		if category.Value == value {
			return true
		}
	}
	return false
}

func validCycle(value string) bool {
	for _, cycle := range renewalCycles {
		if cycle.Value == value {
			return true
		}
	}
	return false
}

func (s *Server) row(item store.Subscription) subscriptionRow {
	today := time.Now().In(s.zone)
	today = time.Date(today.Year(), today.Month(), today.Day(), 0, 0, 0, 0, time.UTC)
	expires := time.Date(item.ExpiresAt.Year(), item.ExpiresAt.Month(), item.ExpiresAt.Day(), 0, 0, 0, 0, time.UTC)
	days := int(expires.Sub(today).Hours() / 24)
	statusLabel, statusClass := "正常", "active"
	switch {
	case item.Archived:
		statusLabel, statusClass = "已归档", "archived"
	case days < 0:
		statusLabel, statusClass = "已过期", "expired"
	case days == 0:
		statusLabel, statusClass = "今天到期", "today"
	case days <= 30:
		statusLabel, statusClass = "即将到期", "upcoming"
	}
	price := "—"
	if item.RenewalPrice.Valid {
		price = fmt.Sprintf("%s %.2f", strings.TrimSpace(item.Currency), item.RenewalPrice.Float64)
	}
	return subscriptionRow{
		Item: item, CategoryLabel: labelFor(categories, item.Category), CycleLabel: labelFor(renewalCycles, item.RenewalCycle),
		ExpiryLabel: item.ExpiresAt.Format("2006-01-02"), DaysRemaining: days,
		DaysOverdue: max(0, -days),
		StatusLabel: statusLabel, StatusClass: statusClass, PriceLabel: price,
	}
}

func labelFor(options []option, value string) string {
	for _, item := range options {
		if item.Value == value {
			return item.Label
		}
	}
	return value
}

func constantTimeEqual(left, right string) bool {
	leftHash, rightHash := sha256.Sum256([]byte(left)), sha256.Sum256([]byte(right))
	return subtle.ConstantTimeCompare(leftHash[:], rightHash[:]) == 1
}

package app

import (
	"html/template"
	"io"
	"math"
	"strings"
	"testing"
	"time"

	"github.com/ACnoway/subtrack/internal/store"
)

func TestNextCycleDate(t *testing.T) {
	tests := []struct {
		name       string
		base       string
		cycle      string
		customDays int
		want       string
		wantValid  bool
	}{
		{name: "month end clamps", base: "2027-01-31", cycle: "monthly", want: "2027-02-28", wantValid: true},
		{name: "leap day annual", base: "2024-02-29", cycle: "annual", want: "2025-02-28", wantValid: true},
		{name: "quarterly", base: "2026-01-31", cycle: "quarterly", want: "2026-04-30", wantValid: true},
		{name: "custom days", base: "2026-10-23", cycle: "custom", customDays: 30, want: "2026-11-22", wantValid: true},
		{name: "custom missing interval", base: "2026-10-23", cycle: "custom", wantValid: false},
		{name: "one time", base: "2026-10-23", cycle: "one_time", wantValid: false},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			base, err := time.Parse("2006-01-02", test.base)
			if err != nil {
				t.Fatal(err)
			}
			got, ok := nextCycleDate(base, test.cycle, test.customDays)
			if ok != test.wantValid {
				t.Fatalf("nextCycleDate valid = %v, want %v", ok, test.wantValid)
			}
			if ok && got.Format("2006-01-02") != test.want {
				t.Fatalf("nextCycleDate = %s, want %s", got.Format("2006-01-02"), test.want)
			}
		})
	}
}

func TestAnnualizedAmount(t *testing.T) {
	if got, ok := annualizedAmount(12, "monthly", 0); !ok || got != 144 {
		t.Fatalf("monthly annualized amount = %v, %v; want 144, true", got, ok)
	}
	got, ok := annualizedAmount(30, "custom", 30)
	if !ok || math.Abs(got-365) > 0.0001 {
		t.Fatalf("custom annualized amount = %v, %v; want 365, true", got, ok)
	}
	if _, ok := annualizedAmount(50, "one_time", 0); ok {
		t.Fatal("one-time price should not be included in recurring estimates")
	}
}

func TestParseBackup(t *testing.T) {
	contents := `{
		"format":"subtrack-backup",
		"version":1,
		"exported_at":"2026-10-23T00:00:00Z",
		"subscriptions":[{
			"reference_id":42,"name":"Example VPS","category":"server","provider":"Example",
			"account_label":"main","description":"","start_date":"2026-01-01","expires_at":"2027-01-01",
			"next_billing_at":"2027-01-01","renewal_cycle":"custom","custom_cycle_days":365,
			"renewal_price":12.5,"currency":"USD","auto_renew":false,"renewal_url":"",
			"priority":2,"tags":"prod","notes":"","archived":false
		}],
		"renewals":[{
			"subscription_ref":42,"subscription_name":"Example VPS","renewed_at":"2026-01-01",
			"previous_expires_at":"2026-01-01","new_expires_at":"2027-01-01","amount":12.5,
			"currency":"USD","notes":"","created_at":"2026-01-01T10:00:00Z"
		}]
	}`
	bundle, err := parseBackup([]byte(contents))
	if err != nil {
		t.Fatalf("parse valid backup: %v", err)
	}
	if len(bundle.Subscriptions) != 1 || len(bundle.Renewals) != 1 {
		t.Fatalf("parsed %d subscriptions and %d renewals, want one each", len(bundle.Subscriptions), len(bundle.Renewals))
	}
	if bundle.Subscriptions[0].Item.CustomCycleDays != 365 || bundle.Renewals[0].SubscriptionRef != 42 {
		t.Fatalf("custom cycle or renewal reference was not preserved: %+v %+v", bundle.Subscriptions[0], bundle.Renewals[0])
	}
}

func TestParseBackupRejectsUnknownFieldsAndInvalidCustomCycle(t *testing.T) {
	base := `{"format":"subtrack-backup","version":1,"subscriptions":[{"reference_id":1,"name":"x","category":"server","expires_at":"2027-01-01","renewal_cycle":"custom","currency":"USD","priority":2,"unknown":true}]}`
	if _, err := parseBackup([]byte(base)); err == nil {
		t.Fatal("backup with an unknown field was accepted")
	}
	invalid := strings.ReplaceAll(base, `,"unknown":true`, `,"custom_cycle_days":0`)
	if _, err := parseBackup([]byte(invalid)); err == nil {
		t.Fatal("custom renewal cycle without an interval was accepted")
	}
}

func TestPhaseThreeTemplatesRender(t *testing.T) {
	tests := []struct {
		page string
		data any
	}{
		{page: "dashboard.html", data: dashboardData{Title: "仪表盘"}},
		{page: "calendar.html", data: calendarData{Title: "到期日历", Weeks: [][]calendarDay{{{Label: "23", Date: "2026-10-23", InMonth: true}}}}},
		{page: "renewal_form.html", data: renewalPageData{
			Title: "确认续费", CSRF: "token", Item: storeSubscriptionForTemplate(),
			Form: renewalForm{RenewedAt: "2026-10-23", NewExpiresAt: "2027-10-23", Currency: "USD"}, Cycles: renewalCycles,
		}},
		{page: "renewals.html", data: renewalsData{Title: "续费历史"}},
		{page: "data.html", data: dataPageData{Title: "数据备份"}},
	}
	for _, test := range tests {
		t.Run(test.page, func(t *testing.T) {
			tmpl, err := template.ParseFS(assets, "templates/layout.html", "templates/"+test.page)
			if err != nil {
				t.Fatalf("parse template: %v", err)
			}
			if err := tmpl.ExecuteTemplate(io.Discard, "layout.html", test.data); err != nil {
				t.Fatalf("render template: %v", err)
			}
		})
	}
}

func storeSubscriptionForTemplate() store.Subscription {
	return store.Subscription{ID: 7, Name: "Test", RenewalCycle: "annual", Currency: "USD"}
}

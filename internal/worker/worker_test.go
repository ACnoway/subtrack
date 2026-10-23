package worker

import (
	"testing"
	"time"

	"github.com/ACnoway/subtrack/internal/store"
)

func TestPlanReminderJobsUsesNotificationTimezone(t *testing.T) {
	zone, err := time.LoadLocation("Asia/Shanghai")
	if err != nil {
		t.Fatalf("load timezone: %v", err)
	}
	today := time.Date(2026, time.October, 23, 0, 0, 0, 0, zone)
	item := store.ReminderSubscription{
		ID: 1, Name: "Example VPS", Category: "server", Priority: 2,
		ExpiresAt: time.Date(2026, time.October, 24, 0, 0, 0, 0, time.UTC),
	}
	planned := planReminderJobs([]store.ReminderSubscription{item}, store.ReminderRules{
		Default: []int{1, 0}, Important: []int{60},
	}, today, zone, 9, 30)
	if len(planned) != 2 {
		t.Fatalf("planned %d jobs, want today and tomorrow reminders", len(planned))
	}
	if planned[0].offset != 0 || planned[1].offset != 1 {
		t.Fatalf("planned offsets = %d, %d; want 0, 1", planned[0].offset, planned[1].offset)
	}
	if got := planned[1].scheduledAt.Format("2006-01-02 15:04 MST"); got != "2026-10-23 09:30 CST" {
		t.Fatalf("scheduled time = %q, want local 09:30", got)
	}
}

func TestPlanReminderJobsAddsImportantAndAfterExpiryRules(t *testing.T) {
	zone := time.UTC
	today := time.Date(2026, time.October, 23, 0, 0, 0, 0, zone)
	important := store.ReminderSubscription{
		ID: 2, Name: "Example Domain", Category: "domain", Priority: 2,
		ExpiresAt: today.AddDate(0, 0, 60),
	}
	postExpiry := store.ReminderSubscription{
		ID: 3, Name: "Expired API", Category: "api", Priority: 2,
		ExpiresAt: today.AddDate(0, 0, -1),
	}
	planned := planReminderJobs([]store.ReminderSubscription{important, postExpiry}, store.ReminderRules{
		Default: []int{30, -1}, Important: []int{60},
	}, today, zone, 8, 0)
	if len(planned) != 4 {
		t.Fatalf("planned %d jobs, want the important and post-expiry reminders", len(planned))
	}
	if planned[0].item.ID != important.ID || planned[0].offset != -1 ||
		planned[1].item.ID != important.ID || planned[1].offset != 30 ||
		planned[2].item.ID != important.ID || planned[2].offset != 60 {
		t.Fatalf("important asset rules were not merged in order: %#v", planned)
	}
	if !planned[2].scheduledAt.Equal(today) {
		t.Fatalf("important 60-day reminder time = %s, want today %s", planned[2].scheduledAt, today)
	}
	if planned[3].item.ID != postExpiry.ID || planned[3].offset != -1 || !planned[3].scheduledAt.Equal(today) {
		t.Fatalf("post-expiry reminder plan = %#v", planned[3])
	}
}

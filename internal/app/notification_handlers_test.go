package app

import (
	"reflect"
	"testing"

	"github.com/ACnoway/subtrack/internal/store"
)

func TestParseReminderRules(t *testing.T) {
	rules, err := parseReminderRules("30, 14; 0 -1", "60, 7")
	if err != nil {
		t.Fatalf("parse valid reminder rules: %v", err)
	}
	if want := []int{-1, 0, 14, 30}; !reflect.DeepEqual(rules.Default, want) {
		t.Fatalf("default rules = %v, want %v", rules.Default, want)
	}
	if want := []int{7, 60}; !reflect.DeepEqual(rules.Important, want) {
		t.Fatalf("important rules = %v, want %v", rules.Important, want)
	}
}

func TestParseReminderRulesRejectsInvalidOffsets(t *testing.T) {
	for _, value := range []string{"1,1", "366", "-366", "later"} {
		t.Run(value, func(t *testing.T) {
			if _, err := parseReminderRules(value, "60"); err == nil {
				t.Fatalf("parseReminderRules(%q) succeeded, want an error", value)
			}
		})
	}
}

func TestValidateIntegrationSettings(t *testing.T) {
	valid := store.IntegrationSettings{
		SMTPHost: "smtp.example.com", SMTPPort: 587, SMTPUsername: "mailer",
		SMTPPassword: "secret", SMTPFrom: "notify@example.com", SMTPEncryption: "starttls",
		TelegramBotToken: "123:token", TelegramBotUsername: "SubTrackBot",
	}
	if err := validateIntegrationSettings(valid); err != nil {
		t.Fatalf("valid integration settings rejected: %v", err)
	}

	tests := []struct {
		name     string
		settings store.IntegrationSettings
	}{
		{name: "auth requires tls", settings: store.IntegrationSettings{SMTPHost: "smtp.example.com", SMTPPort: 25, SMTPUsername: "mailer", SMTPPassword: "secret", SMTPFrom: "notify@example.com", SMTPEncryption: "none"}},
		{name: "host requires sender", settings: store.IntegrationSettings{SMTPHost: "smtp.example.com", SMTPPort: 587, SMTPEncryption: "starttls"}},
		{name: "username requires password", settings: store.IntegrationSettings{SMTPHost: "smtp.example.com", SMTPPort: 587, SMTPUsername: "mailer", SMTPFrom: "notify@example.com", SMTPEncryption: "starttls"}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if err := validateIntegrationSettings(test.settings); err == nil {
				t.Fatal("invalid integration settings were accepted")
			}
		})
	}
}

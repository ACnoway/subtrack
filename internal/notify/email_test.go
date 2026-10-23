package notify

import (
	"strings"
	"testing"
	"time"
)

func TestRenderEmailEscapesAssetContent(t *testing.T) {
	body, err := renderEmail(Reminder{
		Name:       `<script>alert("x")</script>`,
		Provider:   `<b>provider</b>`,
		ExpiresAt:  time.Date(2026, time.October, 24, 0, 0, 0, 0, time.UTC),
		RenewalURL: "https://example.test/renew?a=1&b=2",
		PublicURL:  "https://subtrack.example.test",
	})
	if err != nil {
		t.Fatalf("render email: %v", err)
	}
	if strings.Contains(body, `<script>`) || !strings.Contains(body, `&lt;script&gt;`) {
		t.Fatalf("asset name was not HTML escaped: %s", body)
	}
	if strings.Contains(body, `<b>provider</b>`) || !strings.Contains(body, `&lt;b&gt;provider&lt;/b&gt;`) {
		t.Fatalf("provider was not HTML escaped: %s", body)
	}
	if !strings.Contains(body, `a=1&amp;b=2`) {
		t.Fatalf("renewal URL query was not safely escaped: %s", body)
	}
}

func TestSMTPConfigured(t *testing.T) {
	if (SMTPConfig{Host: "smtp.example.test", Port: 587, From: "bot@example.test"}).Configured() != true {
		t.Fatal("complete SMTP address configuration should be ready")
	}
	if (SMTPConfig{Host: "smtp.example.test", Port: 587}).Configured() {
		t.Fatal("SMTP without a sender should not be ready")
	}
}

package app

import (
	"reflect"
	"testing"
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

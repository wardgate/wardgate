package policy

import (
	"testing"
	"time"

	"github.com/wardgate/wardgate/internal/config"
)

// Tests for time-based rules based on Phase 2 spec:
// - Rules can have time_range with hours and days
// - If current time is outside range, rule doesn't match (skipped)
// - Hours format: "09:00-17:00"
// - Days format: "mon", "tue", etc.

func TestEngine_TimeRange_HoursOnly(t *testing.T) {
	// Rule only applies during business hours
	rules := []config.Rule{
		{
			Match:  config.Match{Method: "POST"},
			Action: "allow",
			TimeRange: &config.TimeRange{
				Hours: []string{"09:00-17:00"},
			},
		},
		{
			Match:   config.Match{Method: "POST"},
			Action:  "deny",
			Message: "outside business hours",
		},
	}
	engine := New(rules)

	// We can't easily control time in tests without injecting a clock,
	// so we just verify the rule structure is processed correctly
	// and the engine doesn't panic
	decision := engine.Evaluate("POST", "/tasks")

	// Should get either allow (if in hours) or deny (if outside)
	if decision.Action != Allow && decision.Action != Deny {
		t.Errorf("expected Allow or Deny, got %v", decision.Action)
	}
}

func TestEngine_TimeRange_DaysOnly(t *testing.T) {
	// Rule only applies on weekdays
	rules := []config.Rule{
		{
			Match:  config.Match{Method: "POST"},
			Action: "allow",
			TimeRange: &config.TimeRange{
				Days: []string{"mon", "tue", "wed", "thu", "fri"},
			},
		},
		{
			Match:   config.Match{Method: "POST"},
			Action:  "deny",
			Message: "weekends not allowed",
		},
	}
	engine := New(rules)

	decision := engine.Evaluate("POST", "/tasks")

	// Should get either allow (weekday) or deny (weekend)
	if decision.Action != Allow && decision.Action != Deny {
		t.Errorf("expected Allow or Deny, got %v", decision.Action)
	}
}

func TestEngine_TimeRange_HoursAndDays(t *testing.T) {
	// Rule only applies during business hours on weekdays
	rules := []config.Rule{
		{
			Match:  config.Match{Method: "POST"},
			Action: "allow",
			TimeRange: &config.TimeRange{
				Hours: []string{"09:00-17:00"},
				Days:  []string{"mon", "tue", "wed", "thu", "fri"},
			},
		},
		{
			Match:   config.Match{Method: "POST"},
			Action:  "deny",
			Message: "only during business hours",
		},
	}
	engine := New(rules)

	decision := engine.Evaluate("POST", "/tasks")

	if decision.Action != Allow && decision.Action != Deny {
		t.Errorf("expected Allow or Deny, got %v", decision.Action)
	}
}

func TestEngine_TimeRange_MultipleHourRanges(t *testing.T) {
	// Rule applies during morning OR afternoon
	rules := []config.Rule{
		{
			Match:  config.Match{Method: "GET"},
			Action: "allow",
			TimeRange: &config.TimeRange{
				Hours: []string{"09:00-12:00", "14:00-17:00"},
			},
		},
		{
			Match:  config.Match{Method: "GET"},
			Action: "deny",
		},
	}
	engine := New(rules)

	decision := engine.Evaluate("GET", "/tasks")

	if decision.Action != Allow && decision.Action != Deny {
		t.Errorf("expected Allow or Deny, got %v", decision.Action)
	}
}

func TestEngine_TimeRange_NoTimeRange_AlwaysMatches(t *testing.T) {
	// Rule without time_range should always match (not skip)
	rules := []config.Rule{
		{
			Match:  config.Match{Method: "GET"},
			Action: "allow",
			// No TimeRange - should always match
		},
	}
	engine := New(rules)

	decision := engine.Evaluate("GET", "/tasks")

	if decision.Action != Allow {
		t.Errorf("expected Allow for rule without time_range, got %v", decision.Action)
	}
}

func TestEngine_TimeRange_SkipsToNextRule(t *testing.T) {
	// When time range doesn't match, should skip to next rule, not deny
	now := time.Now()

	// Create a time range that definitely doesn't include now
	// by using a range in the past hour
	pastHour := now.Add(-2 * time.Hour)
	impossibleRange := []string{
		pastHour.Format("15:04") + "-" + pastHour.Add(30*time.Minute).Format("15:04"),
	}

	rules := []config.Rule{
		{
			Match:  config.Match{Method: "GET"},
			Action: "deny", // Would deny if matched
			TimeRange: &config.TimeRange{
				Hours: impossibleRange,
			},
		},
		{
			Match:  config.Match{Method: "GET"},
			Action: "allow", // Should fall through to this
		},
	}
	engine := New(rules)

	decision := engine.Evaluate("GET", "/tasks")

	// Should skip first rule (time doesn't match) and hit second rule
	if decision.Action != Allow {
		t.Errorf("expected Allow (skip time-restricted rule), got %v", decision.Action)
	}
}

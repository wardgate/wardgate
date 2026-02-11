package auth

import "testing"

func TestAgentAllowed_EmptyList(t *testing.T) {
	// Empty list means all agents allowed
	if !AgentAllowed(nil, "anyone") {
		t.Error("nil list should allow any agent")
	}
	if !AgentAllowed([]string{}, "anyone") {
		t.Error("empty list should allow any agent")
	}
}

func TestAgentAllowed_InList(t *testing.T) {
	if !AgentAllowed([]string{"tessa", "bob"}, "tessa") {
		t.Error("tessa should be allowed")
	}
	if !AgentAllowed([]string{"tessa", "bob"}, "bob") {
		t.Error("bob should be allowed")
	}
}

func TestAgentAllowed_NotInList(t *testing.T) {
	if AgentAllowed([]string{"tessa"}, "bob") {
		t.Error("bob should not be allowed when list is [tessa]")
	}
}

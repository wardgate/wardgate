package grants

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestStore_AddAndList(t *testing.T) {
	s := NewStore("")
	g := Grant{
		AgentID: "tessa",
		Scope:   "conclave:obsidian",
		Match:   GrantMatch{Command: "rg"},
		Action:  "allow",
		Reason:  "test",
	}
	s.Add(g)

	list := s.List()
	if len(list) != 1 {
		t.Fatalf("expected 1 grant, got %d", len(list))
	}
	if list[0].AgentID != "tessa" {
		t.Errorf("expected agent 'tessa', got %q", list[0].AgentID)
	}
	if list[0].ID == "" {
		t.Error("grant should have an auto-generated ID")
	}
}

func TestStore_CheckExec_Matching(t *testing.T) {
	s := NewStore("")
	s.Add(Grant{
		AgentID: "tessa",
		Scope:   "conclave:obsidian",
		Match:   GrantMatch{Command: "rg"},
		Action:  "allow",
	})

	g := s.CheckExec("tessa", "conclave:obsidian", "rg", "", "")
	if g == nil {
		t.Fatal("expected matching grant")
	}
}

func TestStore_CheckExec_NoMatch(t *testing.T) {
	s := NewStore("")
	s.Add(Grant{
		AgentID: "tessa",
		Scope:   "conclave:obsidian",
		Match:   GrantMatch{Command: "rg"},
		Action:  "allow",
	})

	g := s.CheckExec("tessa", "conclave:obsidian", "cat", "", "")
	if g != nil {
		t.Error("expected no match for different command")
	}
}

func TestStore_CheckHTTP_Matching(t *testing.T) {
	s := NewStore("")
	s.Add(Grant{
		AgentID: "tessa",
		Scope:   "endpoint:todoist",
		Match:   GrantMatch{Method: "DELETE", Path: "/tasks/*"},
		Action:  "allow",
	})

	g := s.CheckHTTP("tessa", "endpoint:todoist", "DELETE", "/tasks/123")
	if g == nil {
		t.Fatal("expected matching grant")
	}
}

func TestStore_CheckHTTP_NoMatch(t *testing.T) {
	s := NewStore("")
	s.Add(Grant{
		AgentID: "tessa",
		Scope:   "endpoint:todoist",
		Match:   GrantMatch{Method: "DELETE", Path: "/tasks/*"},
		Action:  "allow",
	})

	g := s.CheckHTTP("tessa", "endpoint:todoist", "GET", "/tasks/123")
	if g != nil {
		t.Error("expected no match for GET")
	}
}

func TestStore_Expiry(t *testing.T) {
	s := NewStore("")
	exp := time.Now().Add(1 * time.Millisecond)
	s.Add(Grant{
		AgentID:   "tessa",
		Scope:     "conclave:obsidian",
		Match:     GrantMatch{Command: "rg"},
		Action:    "allow",
		ExpiresAt: &exp,
	})

	time.Sleep(5 * time.Millisecond)

	g := s.CheckExec("tessa", "conclave:obsidian", "rg", "", "")
	if g != nil {
		t.Error("expired grant should not match")
	}
}

func TestStore_Permanent(t *testing.T) {
	s := NewStore("")
	s.Add(Grant{
		AgentID:   "tessa",
		Scope:     "conclave:obsidian",
		Match:     GrantMatch{Command: "rg"},
		Action:    "allow",
		ExpiresAt: nil, // permanent
	})

	g := s.CheckExec("tessa", "conclave:obsidian", "rg", "", "")
	if g == nil {
		t.Fatal("permanent grant should match")
	}
}

func TestStore_Revoke(t *testing.T) {
	s := NewStore("")
	s.Add(Grant{
		AgentID: "tessa",
		Scope:   "conclave:obsidian",
		Match:   GrantMatch{Command: "rg"},
		Action:  "allow",
	})

	grants := s.List()
	if len(grants) != 1 {
		t.Fatalf("expected 1 grant, got %d", len(grants))
	}

	if err := s.Revoke(grants[0].ID); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(s.List()) != 0 {
		t.Error("grant should be revoked")
	}
}

func TestStore_AgentWildcard(t *testing.T) {
	s := NewStore("")
	s.Add(Grant{
		AgentID: "*",
		Scope:   "conclave:obsidian",
		Match:   GrantMatch{Command: "rg"},
		Action:  "allow",
	})

	g := s.CheckExec("anyone", "conclave:obsidian", "rg", "", "")
	if g == nil {
		t.Fatal("wildcard agent should match any agent")
	}
}

func TestStore_AgentSpecific(t *testing.T) {
	s := NewStore("")
	s.Add(Grant{
		AgentID: "tessa",
		Scope:   "conclave:obsidian",
		Match:   GrantMatch{Command: "rg"},
		Action:  "allow",
	})

	g := s.CheckExec("bob", "conclave:obsidian", "rg", "", "")
	if g != nil {
		t.Error("grant for tessa should not match bob")
	}
}

func TestStore_Persistence(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "grants.json")

	s1 := NewStore(path)
	s1.Add(Grant{
		AgentID: "tessa",
		Scope:   "conclave:obsidian",
		Match:   GrantMatch{Command: "rg"},
		Action:  "allow",
		Reason:  "persisted",
	})

	// Create new store from same file
	s2, err := LoadStore(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	list := s2.List()
	if len(list) != 1 {
		t.Fatalf("expected 1 grant after reload, got %d", len(list))
	}
	if list[0].Reason != "persisted" {
		t.Errorf("expected reason 'persisted', got %q", list[0].Reason)
	}
}

func TestStore_PruneExpired(t *testing.T) {
	s := NewStore("")
	expired := time.Now().Add(-1 * time.Hour)
	s.Add(Grant{
		AgentID:   "tessa",
		Scope:     "conclave:obsidian",
		Match:     GrantMatch{Command: "old"},
		Action:    "allow",
		ExpiresAt: &expired,
	})
	s.Add(Grant{
		AgentID: "tessa",
		Scope:   "conclave:obsidian",
		Match:   GrantMatch{Command: "active"},
		Action:  "allow",
		// no expiry = permanent
	})

	s.Prune()

	list := s.List()
	if len(list) != 1 {
		t.Fatalf("expected 1 grant after prune, got %d", len(list))
	}
	if list[0].Match.Command != "active" {
		t.Errorf("expected 'active' grant, got %q", list[0].Match.Command)
	}
}

func TestStore_PersistenceFileCreated(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "grants.json")

	s := NewStore(path)
	s.Add(Grant{AgentID: "test", Scope: "test", Action: "allow"})

	if _, err := os.Stat(path); os.IsNotExist(err) {
		t.Error("grants file should be created on Add")
	}
}

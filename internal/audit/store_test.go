package audit

import (
	"testing"
	"time"
)

func TestStore_AddAndQuery(t *testing.T) {
	store := NewStore(100)

	// Add some entries
	store.Add(StoredEntry{
		Entry: Entry{
			RequestID: "req1",
			Endpoint:  "todoist",
			Method:    "GET",
			Path:      "/tasks",
			AgentID:   "agent1",
			Decision:  "allow",
		},
		Timestamp: time.Now(),
	})
	store.Add(StoredEntry{
		Entry: Entry{
			RequestID: "req2",
			Endpoint:  "github",
			Method:    "POST",
			Path:      "/issues",
			AgentID:   "agent1",
			Decision:  "allow",
		},
		Timestamp: time.Now(),
	})
	store.Add(StoredEntry{
		Entry: Entry{
			RequestID: "req3",
			Endpoint:  "todoist",
			Method:    "DELETE",
			Path:      "/tasks/123",
			AgentID:   "agent2",
			Decision:  "deny",
		},
		Timestamp: time.Now(),
	})

	// Query all
	results := store.Query(QueryParams{Limit: 10})
	if len(results) != 3 {
		t.Errorf("expected 3 results, got %d", len(results))
	}

	// Results should be newest first
	if results[0].RequestID != "req3" {
		t.Errorf("expected newest first, got %s", results[0].RequestID)
	}
}

func TestStore_QueryByEndpoint(t *testing.T) {
	store := NewStore(100)

	store.Add(StoredEntry{Entry: Entry{RequestID: "req1", Endpoint: "todoist"}, Timestamp: time.Now()})
	store.Add(StoredEntry{Entry: Entry{RequestID: "req2", Endpoint: "github"}, Timestamp: time.Now()})
	store.Add(StoredEntry{Entry: Entry{RequestID: "req3", Endpoint: "todoist"}, Timestamp: time.Now()})

	results := store.Query(QueryParams{Endpoint: "todoist", Limit: 10})
	if len(results) != 2 {
		t.Errorf("expected 2 todoist results, got %d", len(results))
	}
	for _, r := range results {
		if r.Endpoint != "todoist" {
			t.Errorf("expected todoist endpoint, got %s", r.Endpoint)
		}
	}
}

func TestStore_QueryByAgent(t *testing.T) {
	store := NewStore(100)

	store.Add(StoredEntry{Entry: Entry{RequestID: "req1", AgentID: "agent1"}, Timestamp: time.Now()})
	store.Add(StoredEntry{Entry: Entry{RequestID: "req2", AgentID: "agent2"}, Timestamp: time.Now()})
	store.Add(StoredEntry{Entry: Entry{RequestID: "req3", AgentID: "agent1"}, Timestamp: time.Now()})

	results := store.Query(QueryParams{AgentID: "agent1", Limit: 10})
	if len(results) != 2 {
		t.Errorf("expected 2 agent1 results, got %d", len(results))
	}
}

func TestStore_QueryByDecision(t *testing.T) {
	store := NewStore(100)

	store.Add(StoredEntry{Entry: Entry{RequestID: "req1", Decision: "allow"}, Timestamp: time.Now()})
	store.Add(StoredEntry{Entry: Entry{RequestID: "req2", Decision: "deny"}, Timestamp: time.Now()})
	store.Add(StoredEntry{Entry: Entry{RequestID: "req3", Decision: "allow"}, Timestamp: time.Now()})

	results := store.Query(QueryParams{Decision: "deny", Limit: 10})
	if len(results) != 1 {
		t.Errorf("expected 1 deny result, got %d", len(results))
	}
	if results[0].RequestID != "req2" {
		t.Errorf("expected req2, got %s", results[0].RequestID)
	}
}

func TestStore_QueryByMethod(t *testing.T) {
	store := NewStore(100)

	store.Add(StoredEntry{Entry: Entry{RequestID: "req1", Method: "GET"}, Timestamp: time.Now()})
	store.Add(StoredEntry{Entry: Entry{RequestID: "req2", Method: "POST"}, Timestamp: time.Now()})
	store.Add(StoredEntry{Entry: Entry{RequestID: "req3", Method: "GET"}, Timestamp: time.Now()})

	results := store.Query(QueryParams{Method: "POST", Limit: 10})
	if len(results) != 1 {
		t.Errorf("expected 1 POST result, got %d", len(results))
	}
}

func TestStore_QueryLimit(t *testing.T) {
	store := NewStore(100)

	for i := 0; i < 20; i++ {
		store.Add(StoredEntry{Entry: Entry{RequestID: "req"}, Timestamp: time.Now()})
	}

	results := store.Query(QueryParams{Limit: 5})
	if len(results) != 5 {
		t.Errorf("expected 5 results with limit, got %d", len(results))
	}
}

func TestStore_QueryBefore(t *testing.T) {
	store := NewStore(100)

	t1 := time.Now().Add(-3 * time.Hour)
	t2 := time.Now().Add(-2 * time.Hour)
	t3 := time.Now().Add(-1 * time.Hour)

	store.Add(StoredEntry{Entry: Entry{RequestID: "req1"}, Timestamp: t1})
	store.Add(StoredEntry{Entry: Entry{RequestID: "req2"}, Timestamp: t2})
	store.Add(StoredEntry{Entry: Entry{RequestID: "req3"}, Timestamp: t3})

	// Query before t3 (should get req1 and req2)
	results := store.Query(QueryParams{Before: t3, Limit: 10})
	if len(results) != 2 {
		t.Errorf("expected 2 results before t3, got %d", len(results))
	}
}

func TestStore_RingBufferOverflow(t *testing.T) {
	store := NewStore(5) // Small buffer

	// Add more entries than capacity
	for i := 0; i < 10; i++ {
		store.Add(StoredEntry{Entry: Entry{RequestID: "req" + string(rune('0'+i))}, Timestamp: time.Now()})
	}

	results := store.Query(QueryParams{Limit: 100})
	if len(results) != 5 {
		t.Errorf("expected 5 results (buffer size), got %d", len(results))
	}

	// Should have the newest entries (req5-req9)
	// Note: req5 is '5', req9 is '9' in ASCII
	if results[0].RequestID != "req9" {
		t.Errorf("expected newest entry req9, got %s", results[0].RequestID)
	}
}

func TestStore_CombinedFilters(t *testing.T) {
	store := NewStore(100)

	store.Add(StoredEntry{Entry: Entry{RequestID: "req1", Endpoint: "todoist", AgentID: "agent1", Decision: "allow"}, Timestamp: time.Now()})
	store.Add(StoredEntry{Entry: Entry{RequestID: "req2", Endpoint: "todoist", AgentID: "agent2", Decision: "deny"}, Timestamp: time.Now()})
	store.Add(StoredEntry{Entry: Entry{RequestID: "req3", Endpoint: "github", AgentID: "agent1", Decision: "allow"}, Timestamp: time.Now()})
	store.Add(StoredEntry{Entry: Entry{RequestID: "req4", Endpoint: "todoist", AgentID: "agent1", Decision: "deny"}, Timestamp: time.Now()})

	// Filter by endpoint AND agent AND decision
	results := store.Query(QueryParams{
		Endpoint: "todoist",
		AgentID:  "agent1",
		Decision: "allow",
		Limit:    10,
	})
	if len(results) != 1 {
		t.Errorf("expected 1 result with combined filters, got %d", len(results))
	}
	if results[0].RequestID != "req1" {
		t.Errorf("expected req1, got %s", results[0].RequestID)
	}
}

func TestStore_BodyStorage(t *testing.T) {
	store := NewStore(100)

	body := `{"to": ["test@example.com"], "subject": "Hello"}`
	store.Add(StoredEntry{
		Entry:       Entry{RequestID: "req1", Endpoint: "smtp"},
		Timestamp:   time.Now(),
		RequestBody: body,
	})

	results := store.Query(QueryParams{Limit: 10})
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	if results[0].RequestBody != body {
		t.Errorf("expected body to be stored, got %s", results[0].RequestBody)
	}
}

func TestStore_GetEndpoints(t *testing.T) {
	store := NewStore(100)

	store.Add(StoredEntry{Entry: Entry{Endpoint: "todoist"}, Timestamp: time.Now()})
	store.Add(StoredEntry{Entry: Entry{Endpoint: "github"}, Timestamp: time.Now()})
	store.Add(StoredEntry{Entry: Entry{Endpoint: "todoist"}, Timestamp: time.Now()})
	store.Add(StoredEntry{Entry: Entry{Endpoint: "smtp"}, Timestamp: time.Now()})

	endpoints := store.GetEndpoints()
	if len(endpoints) != 3 {
		t.Errorf("expected 3 unique endpoints, got %d", len(endpoints))
	}

	// Check all expected endpoints are present
	found := make(map[string]bool)
	for _, ep := range endpoints {
		found[ep] = true
	}
	for _, expected := range []string{"todoist", "github", "smtp"} {
		if !found[expected] {
			t.Errorf("expected endpoint %s not found", expected)
		}
	}
}

func TestStore_GetAgents(t *testing.T) {
	store := NewStore(100)

	store.Add(StoredEntry{Entry: Entry{AgentID: "agent1"}, Timestamp: time.Now()})
	store.Add(StoredEntry{Entry: Entry{AgentID: "agent2"}, Timestamp: time.Now()})
	store.Add(StoredEntry{Entry: Entry{AgentID: "agent1"}, Timestamp: time.Now()})

	agents := store.GetAgents()
	if len(agents) != 2 {
		t.Errorf("expected 2 unique agents, got %d", len(agents))
	}
}

func TestStore_Concurrent(t *testing.T) {
	store := NewStore(100)

	// Test concurrent writes
	done := make(chan bool, 10)
	for i := 0; i < 10; i++ {
		go func(id int) {
			for j := 0; j < 100; j++ {
				store.Add(StoredEntry{Entry: Entry{RequestID: "req"}, Timestamp: time.Now()})
			}
			done <- true
		}(i)
	}

	// Wait for all goroutines
	for i := 0; i < 10; i++ {
		<-done
	}

	// Should have entries (up to buffer size)
	results := store.Query(QueryParams{Limit: 200})
	if len(results) != 100 {
		t.Errorf("expected 100 results (buffer size), got %d", len(results))
	}
}

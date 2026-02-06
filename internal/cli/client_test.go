package cli

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestClient_ResolveURL_Relative(t *testing.T) {
	client, err := NewClient("http://wardgate:8080", "key", ClientOptions{})
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	url, err := client.ResolveURL("/todoist/tasks")
	if err != nil {
		t.Fatalf("ResolveURL: %v", err)
	}
	if url != "http://wardgate:8080/todoist/tasks" {
		t.Errorf("expected http://wardgate:8080/todoist/tasks, got %q", url)
	}
}

func TestClient_ResolveURL_AbsoluteMatching(t *testing.T) {
	client, err := NewClient("http://wardgate:8080", "key", ClientOptions{})
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	url, err := client.ResolveURL("http://wardgate:8080/todoist/tasks")
	if err != nil {
		t.Fatalf("ResolveURL: %v", err)
	}
	if url != "http://wardgate:8080/todoist/tasks" {
		t.Errorf("expected http://wardgate:8080/todoist/tasks, got %q", url)
	}
}

func TestClient_ResolveURL_AbsoluteRejected(t *testing.T) {
	client, err := NewClient("http://wardgate:8080", "key", ClientOptions{})
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	_, err = client.ResolveURL("http://evil.com/todoist/tasks")
	if err == nil {
		t.Error("expected error for non-matching URL")
	}
}

func TestClient_Do_AddsAuth(t *testing.T) {
	var authHeader string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		authHeader = r.Header.Get("Authorization")
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	client, err := NewClient(srv.URL, "test-key", ClientOptions{})
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}

	req, err := client.BuildRequest("/", RequestOptions{Method: http.MethodGet})
	if err != nil {
		t.Fatalf("BuildRequest: %v", err)
	}

	_, err = client.Do(req)
	if err != nil {
		t.Fatalf("Do: %v", err)
	}

	if authHeader != "Bearer test-key" {
		t.Errorf("expected Authorization: Bearer test-key, got %q", authHeader)
	}
}

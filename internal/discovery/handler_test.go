package discovery

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestHandler_ListEndpoints(t *testing.T) {
	tests := []struct {
		name      string
		endpoints []EndpointInfo
		want      []EndpointInfo
	}{
		{
			name:      "empty endpoints",
			endpoints: []EndpointInfo{},
			want:      []EndpointInfo{},
		},
		{
			name: "single endpoint with description",
			endpoints: []EndpointInfo{
				{Name: "todoist", Description: "Personal task manager"},
			},
			want: []EndpointInfo{
				{Name: "todoist", Description: "Personal task manager"},
			},
		},
		{
			name: "multiple endpoints",
			endpoints: []EndpointInfo{
				{Name: "todoist", Description: "Todoist"},
				{Name: "mail-read", Description: "IMAP"},
				{Name: "mail-send", Description: "SMTP"},
			},
			want: []EndpointInfo{
				{Name: "todoist", Description: "Todoist"},
				{Name: "mail-read", Description: "IMAP"},
				{Name: "mail-send", Description: "SMTP"},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := NewHandler(tt.endpoints)

			req := httptest.NewRequest("GET", "/endpoints", nil)
			rec := httptest.NewRecorder()

			h.ServeHTTP(rec, req)

			if rec.Code != http.StatusOK {
				t.Errorf("status = %d, want %d", rec.Code, http.StatusOK)
			}

			if ct := rec.Header().Get("Content-Type"); ct != "application/json" {
				t.Errorf("Content-Type = %q, want %q", ct, "application/json")
			}

			var resp EndpointsResponse
			if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
				t.Fatalf("failed to decode response: %v", err)
			}

			if len(resp.Endpoints) != len(tt.want) {
				t.Errorf("got %d endpoints, want %d", len(resp.Endpoints), len(tt.want))
			}

			for i, ep := range resp.Endpoints {
				if ep.Name != tt.want[i].Name {
					t.Errorf("endpoint[%d].Name = %q, want %q", i, ep.Name, tt.want[i].Name)
				}
				if ep.Description != tt.want[i].Description {
					t.Errorf("endpoint[%d].Description = %q, want %q", i, ep.Description, tt.want[i].Description)
				}
			}
		})
	}
}

func TestHandler_NotFound(t *testing.T) {
	h := NewHandler([]EndpointInfo{})

	req := httptest.NewRequest("GET", "/other", nil)
	rec := httptest.NewRecorder()

	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusNotFound)
	}
}

func TestHandler_MethodNotAllowed(t *testing.T) {
	h := NewHandler([]EndpointInfo{})

	req := httptest.NewRequest("POST", "/endpoints", nil)
	rec := httptest.NewRecorder()

	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusMethodNotAllowed)
	}
}

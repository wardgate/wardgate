package notify

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

// Channel defines a notification channel.
type Channel interface {
	Send(ctx context.Context, msg Message) error
}

// Message represents a notification message.
type Message struct {
	Title      string `json:"title"`
	Body       string `json:"body"`
	RequestID  string `json:"request_id"`
	Endpoint   string `json:"endpoint"`
	Method     string `json:"method"`
	Path       string `json:"path"`
	AgentID    string `json:"agent_id,omitempty"`
	ApproveURL string `json:"approve_url,omitempty"`
	DenyURL    string `json:"deny_url,omitempty"`
}

// WebhookChannel sends notifications via HTTP webhook.
type WebhookChannel struct {
	URL     string
	Headers map[string]string
	client  *http.Client
}

// NewWebhookChannel creates a new webhook notification channel.
func NewWebhookChannel(url string, headers map[string]string) *WebhookChannel {
	return &WebhookChannel{
		URL:     url,
		Headers: headers,
		client: &http.Client{
			Timeout: 10 * time.Second,
		},
	}
}

// Send sends a notification via webhook.
func (w *WebhookChannel) Send(ctx context.Context, msg Message) error {
	body, err := json.Marshal(msg)
	if err != nil {
		return fmt.Errorf("marshal message: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", w.URL, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	for k, v := range w.Headers {
		req.Header.Set(k, v)
	}

	resp, err := w.client.Do(req)
	if err != nil {
		return fmt.Errorf("send request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		return fmt.Errorf("webhook returned status %d", resp.StatusCode)
	}

	return nil
}

// SlackChannel sends notifications to Slack via webhook.
type SlackChannel struct {
	WebhookURL string
	client     *http.Client
}

// NewSlackChannel creates a new Slack notification channel.
func NewSlackChannel(webhookURL string) *SlackChannel {
	return &SlackChannel{
		WebhookURL: webhookURL,
		client: &http.Client{
			Timeout: 10 * time.Second,
		},
	}
}

// Send sends a notification to Slack.
func (s *SlackChannel) Send(ctx context.Context, msg Message) error {
	// Build Slack message with blocks for approve/deny buttons
	payload := map[string]interface{}{
		"text": fmt.Sprintf("%s: %s", msg.Title, msg.Body),
		"blocks": []map[string]interface{}{
			{
				"type": "header",
				"text": map[string]string{
					"type": "plain_text",
					"text": msg.Title,
				},
			},
			{
				"type": "section",
				"fields": []map[string]string{
					{"type": "mrkdwn", "text": fmt.Sprintf("*Endpoint:* %s", msg.Endpoint)},
					{"type": "mrkdwn", "text": fmt.Sprintf("*Agent:* %s", msg.AgentID)},
					{"type": "mrkdwn", "text": fmt.Sprintf("*Method:* %s", msg.Method)},
					{"type": "mrkdwn", "text": fmt.Sprintf("*Path:* %s", msg.Path)},
				},
			},
			{
				"type": "section",
				"text": map[string]string{
					"type": "mrkdwn",
					"text": msg.Body,
				},
			},
		},
	}

	// Add action buttons if URLs provided
	if msg.ApproveURL != "" || msg.DenyURL != "" {
		buttons := []map[string]interface{}{}
		if msg.ApproveURL != "" {
			buttons = append(buttons, map[string]interface{}{
				"type": "button",
				"text": map[string]string{"type": "plain_text", "text": "Approve"},
				"style": "primary",
				"url":   msg.ApproveURL,
			})
		}
		if msg.DenyURL != "" {
			buttons = append(buttons, map[string]interface{}{
				"type": "button",
				"text": map[string]string{"type": "plain_text", "text": "Deny"},
				"style": "danger",
				"url":   msg.DenyURL,
			})
		}
		payload["blocks"] = append(payload["blocks"].([]map[string]interface{}), map[string]interface{}{
			"type":     "actions",
			"elements": buttons,
		})
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("marshal slack message: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", s.WebhookURL, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := s.client.Do(req)
	if err != nil {
		return fmt.Errorf("send request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		return fmt.Errorf("slack returned status %d", resp.StatusCode)
	}

	return nil
}

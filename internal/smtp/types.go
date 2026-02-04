package smtp

import (
	"context"
	"errors"
	"time"

	"github.com/wardgate/wardgate/internal/approval"
)

var (
	ErrSendFailed        = errors.New("failed to send email")
	ErrConnectionFailed  = errors.New("failed to connect to SMTP server")
	ErrAuthFailed        = errors.New("SMTP authentication failed")
	ErrRecipientBlocked  = errors.New("recipient not allowed")
	ErrContentBlocked    = errors.New("email content blocked by filter")
)

// Email represents an email message to be sent.
type Email struct {
	From     string    `json:"from,omitempty"`
	To       []string  `json:"to"`
	Cc       []string  `json:"cc,omitempty"`
	Bcc      []string  `json:"bcc,omitempty"`
	ReplyTo  string    `json:"reply_to,omitempty"`
	Subject  string    `json:"subject"`
	Body     string    `json:"body"`
	HTMLBody string    `json:"html_body,omitempty"`
	Date     time.Time `json:"date,omitempty"`
}

// SendRequest is the JSON request body for sending an email.
type SendRequest struct {
	To       []string `json:"to"`
	Cc       []string `json:"cc,omitempty"`
	Bcc      []string `json:"bcc,omitempty"`
	ReplyTo  string   `json:"reply_to,omitempty"`
	Subject  string   `json:"subject"`
	Body     string   `json:"body"`
	HTMLBody string   `json:"html_body,omitempty"`
}

// SendResponse is the JSON response after sending an email.
type SendResponse struct {
	Status    string `json:"status"`
	MessageID string `json:"message_id,omitempty"`
	Error     string `json:"error,omitempty"`
}

// Client is the interface for sending emails via SMTP.
type Client interface {
	Send(ctx context.Context, email Email) error
	Close() error
}

// ConnectionConfig holds SMTP connection parameters.
type ConnectionConfig struct {
	Host               string
	Port               int
	Username           string
	Password           string
	TLS                bool   // Use implicit TLS (SMTPS on port 465)
	StartTLS           bool   // Use STARTTLS upgrade
	InsecureSkipVerify bool   // Skip TLS cert verification (for self-signed)
	From               string // Default from address
}

// ApprovalRequester is the interface for requesting approval.
type ApprovalRequester interface {
	RequestApproval(ctx context.Context, endpoint, method, path, agentID string) (bool, error)
	RequestApprovalWithContent(ctx context.Context, req approval.ApprovalRequest) (bool, error)
}

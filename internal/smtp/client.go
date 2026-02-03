package smtp

import (
	"context"
	"crypto/tls"
	"fmt"
	"net"
	"net/smtp"
	"strings"
	"time"
)

// SMTPClient is a real SMTP client implementation.
type SMTPClient struct {
	config ConnectionConfig
}

// NewSMTPClient creates a new SMTP client.
func NewSMTPClient(cfg ConnectionConfig) *SMTPClient {
	return &SMTPClient{
		config: cfg,
	}
}

// Send sends an email via SMTP.
func (c *SMTPClient) Send(ctx context.Context, email Email) error {
	// Set from address
	from := email.From
	if from == "" {
		from = c.config.From
	}
	if from == "" {
		return fmt.Errorf("from address required")
	}

	// Collect all recipients
	var recipients []string
	recipients = append(recipients, email.To...)
	recipients = append(recipients, email.Cc...)
	recipients = append(recipients, email.Bcc...)

	if len(recipients) == 0 {
		return fmt.Errorf("at least one recipient required")
	}

	// Build message
	msg := c.buildMessage(from, email)

	// Send based on connection type
	if c.config.TLS {
		return c.sendTLS(ctx, from, recipients, msg)
	}
	return c.sendPlain(ctx, from, recipients, msg)
}

func (c *SMTPClient) buildMessage(from string, email Email) []byte {
	var sb strings.Builder

	// Headers
	sb.WriteString(fmt.Sprintf("From: %s\r\n", from))
	sb.WriteString(fmt.Sprintf("To: %s\r\n", strings.Join(email.To, ", ")))
	if len(email.Cc) > 0 {
		sb.WriteString(fmt.Sprintf("Cc: %s\r\n", strings.Join(email.Cc, ", ")))
	}
	if email.ReplyTo != "" {
		sb.WriteString(fmt.Sprintf("Reply-To: %s\r\n", email.ReplyTo))
	}
	sb.WriteString(fmt.Sprintf("Subject: %s\r\n", email.Subject))
	sb.WriteString(fmt.Sprintf("Date: %s\r\n", time.Now().Format(time.RFC1123Z)))

	// Handle HTML + plain text (multipart) or plain text only
	if email.HTMLBody != "" {
		boundary := "boundary-wardgate-" + fmt.Sprintf("%d", time.Now().UnixNano())
		sb.WriteString("MIME-Version: 1.0\r\n")
		sb.WriteString(fmt.Sprintf("Content-Type: multipart/alternative; boundary=\"%s\"\r\n", boundary))
		sb.WriteString("\r\n")

		// Plain text part
		sb.WriteString(fmt.Sprintf("--%s\r\n", boundary))
		sb.WriteString("Content-Type: text/plain; charset=\"utf-8\"\r\n")
		sb.WriteString("\r\n")
		sb.WriteString(email.Body)
		sb.WriteString("\r\n")

		// HTML part
		sb.WriteString(fmt.Sprintf("--%s\r\n", boundary))
		sb.WriteString("Content-Type: text/html; charset=\"utf-8\"\r\n")
		sb.WriteString("\r\n")
		sb.WriteString(email.HTMLBody)
		sb.WriteString("\r\n")

		sb.WriteString(fmt.Sprintf("--%s--\r\n", boundary))
	} else {
		sb.WriteString("MIME-Version: 1.0\r\n")
		sb.WriteString("Content-Type: text/plain; charset=\"utf-8\"\r\n")
		sb.WriteString("\r\n")
		sb.WriteString(email.Body)
	}

	return []byte(sb.String())
}

func (c *SMTPClient) sendTLS(ctx context.Context, from string, recipients []string, msg []byte) error {
	addr := fmt.Sprintf("%s:%d", c.config.Host, c.config.Port)

	tlsConfig := &tls.Config{
		ServerName:         c.config.Host,
		InsecureSkipVerify: c.config.InsecureSkipVerify,
	}

	conn, err := tls.Dial("tcp", addr, tlsConfig)
	if err != nil {
		return fmt.Errorf("TLS dial failed: %w", err)
	}
	defer conn.Close()

	client, err := smtp.NewClient(conn, c.config.Host)
	if err != nil {
		return fmt.Errorf("SMTP client creation failed: %w", err)
	}
	defer client.Close()

	return c.sendWithClient(client, from, recipients, msg)
}

func (c *SMTPClient) sendPlain(ctx context.Context, from string, recipients []string, msg []byte) error {
	addr := fmt.Sprintf("%s:%d", c.config.Host, c.config.Port)

	conn, err := net.Dial("tcp", addr)
	if err != nil {
		return fmt.Errorf("dial failed: %w", err)
	}
	defer conn.Close()

	client, err := smtp.NewClient(conn, c.config.Host)
	if err != nil {
		return fmt.Errorf("SMTP client creation failed: %w", err)
	}
	defer client.Close()

	// STARTTLS if configured
	if c.config.StartTLS {
		tlsConfig := &tls.Config{
			ServerName:         c.config.Host,
			InsecureSkipVerify: c.config.InsecureSkipVerify,
		}
		if err := client.StartTLS(tlsConfig); err != nil {
			return fmt.Errorf("STARTTLS failed: %w", err)
		}
	}

	return c.sendWithClient(client, from, recipients, msg)
}

func (c *SMTPClient) sendWithClient(client *smtp.Client, from string, recipients []string, msg []byte) error {
	// Auth if credentials provided
	if c.config.Username != "" {
		auth := smtp.PlainAuth("", c.config.Username, c.config.Password, c.config.Host)
		if err := client.Auth(auth); err != nil {
			return fmt.Errorf("auth failed: %w", err)
		}
	}

	// Set sender
	if err := client.Mail(from); err != nil {
		return fmt.Errorf("MAIL FROM failed: %w", err)
	}

	// Set recipients
	for _, rcpt := range recipients {
		if err := client.Rcpt(rcpt); err != nil {
			return fmt.Errorf("RCPT TO failed for %s: %w", rcpt, err)
		}
	}

	// Send data
	w, err := client.Data()
	if err != nil {
		return fmt.Errorf("DATA failed: %w", err)
	}

	if _, err := w.Write(msg); err != nil {
		return fmt.Errorf("write failed: %w", err)
	}

	if err := w.Close(); err != nil {
		return fmt.Errorf("close data failed: %w", err)
	}

	return client.Quit()
}

// Close closes the SMTP client (no-op for stateless client).
func (c *SMTPClient) Close() error {
	return nil
}

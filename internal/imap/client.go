package imap

import (
	"context"
	"crypto/tls"
	"fmt"
	"io"
	"net"

	"github.com/emersion/go-imap/v2"
	"github.com/emersion/go-imap/v2/imapclient"
)

// IMAPDialer creates real IMAP connections.
type IMAPDialer struct{}

// NewIMAPDialer creates a new IMAP dialer.
func NewIMAPDialer() *IMAPDialer {
	return &IMAPDialer{}
}

// Dial connects to an IMAP server.
func (d *IMAPDialer) Dial(ctx context.Context, cfg ConnectionConfig) (Connection, error) {
	addr := fmt.Sprintf("%s:%d", cfg.Host, cfg.Port)

	var client *imapclient.Client
	var err error

	opts := &imapclient.Options{}

	if cfg.TLS {
		tlsConfig := &tls.Config{
			ServerName:         cfg.Host,
			InsecureSkipVerify: cfg.InsecureSkipVerify,
		}
		conn, dialErr := tls.Dial("tcp", addr, tlsConfig)
		if dialErr != nil {
			return nil, fmt.Errorf("failed to connect: %w", dialErr)
		}
		client = imapclient.New(conn, opts)
	} else {
		conn, dialErr := net.Dial("tcp", addr)
		if dialErr != nil {
			return nil, fmt.Errorf("failed to connect: %w", dialErr)
		}
		client = imapclient.New(conn, opts)
	}
	if err != nil {
		return nil, fmt.Errorf("failed to connect: %w", err)
	}

	// Login
	if err := client.Login(cfg.Username, cfg.Password).Wait(); err != nil {
		client.Close()
		return nil, fmt.Errorf("login failed: %w", err)
	}

	return &imapConnection{
		client:   client,
		host:     cfg.Host,
		username: cfg.Username,
	}, nil
}

// imapConnection wraps an IMAP client connection.
type imapConnection struct {
	client   *imapclient.Client
	host     string
	username string
}

func (c *imapConnection) IsAlive() bool {
	// Simple check - try to get the client state
	return c.client != nil
}

func (c *imapConnection) Close() error {
	if c.client != nil {
		return c.client.Close()
	}
	return nil
}

func (c *imapConnection) ListFolders(ctx context.Context) ([]Folder, error) {
	listCmd := c.client.List("", "*", nil)
	defer listCmd.Close()

	var folders []Folder
	for {
		mbox := listCmd.Next()
		if mbox == nil {
			break
		}
		folders = append(folders, Folder{
			Name:      mbox.Mailbox,
			Delimiter: string(mbox.Delim),
		})
	}

	if err := listCmd.Close(); err != nil {
		return nil, fmt.Errorf("list folders failed: %w", err)
	}
	return folders, nil
}

func (c *imapConnection) SelectFolder(ctx context.Context, folder string) (*FolderStatus, error) {
	mbox, err := c.client.Select(folder, nil).Wait()
	if err != nil {
		return nil, fmt.Errorf("select folder failed: %w", err)
	}
	return &FolderStatus{
		Name:     folder,
		Messages: mbox.NumMessages,
	}, nil
}

func (c *imapConnection) FetchMessages(ctx context.Context, opts FetchOptions) ([]Message, error) {
	// Select the folder first
	mbox, err := c.client.Select(opts.Folder, nil).Wait()
	if err != nil {
		return nil, fmt.Errorf("select folder failed: %w", err)
	}

	if mbox.NumMessages == 0 {
		return []Message{}, nil
	}

	// Determine which messages to fetch
	limit := opts.Limit
	if limit <= 0 || uint32(limit) > mbox.NumMessages {
		limit = int(mbox.NumMessages)
	}

	// Fetch most recent messages (highest sequence numbers)
	start := mbox.NumMessages - uint32(limit) + 1
	seqSet := imap.SeqSet{}
	seqSet.AddRange(start, mbox.NumMessages)

	// Fetch envelope data
	fetchOptions := &imap.FetchOptions{
		Envelope: true,
		Flags:    true,
		UID:      true,
	}

	fetchCmd := c.client.Fetch(seqSet, fetchOptions)
	defer fetchCmd.Close()

	var messages []Message
	for {
		msg := fetchCmd.Next()
		if msg == nil {
			break
		}

		// Collect the fetched data
		var uid imap.UID
		var envelope *imap.Envelope
		var flags []imap.Flag
		var seen bool

		for {
			item := msg.Next()
			if item == nil {
				break
			}
			switch data := item.(type) {
			case imapclient.FetchItemDataUID:
				uid = data.UID
			case imapclient.FetchItemDataEnvelope:
				envelope = data.Envelope
			case imapclient.FetchItemDataFlags:
				flags = data.Flags
				for _, f := range flags {
					if f == imap.FlagSeen {
						seen = true
						break
					}
				}
			}
		}

		if envelope != nil {
			from := ""
			if len(envelope.From) > 0 {
				from = envelope.From[0].Addr()
			}

			var to []string
			for _, addr := range envelope.To {
				to = append(to, addr.Addr())
			}

			m := Message{
				UID:     uint32(uid),
				Subject: envelope.Subject,
				From:    from,
				To:      to,
				Date:    envelope.Date,
				Seen:    seen,
			}

			// Apply date filters
			if opts.Since != nil && m.Date.Before(*opts.Since) {
				continue
			}
			if opts.Before != nil && m.Date.After(*opts.Before) {
				continue
			}

			messages = append(messages, m)
		}
	}

	if err := fetchCmd.Close(); err != nil {
		return nil, fmt.Errorf("fetch failed: %w", err)
	}

	return messages, nil
}

func (c *imapConnection) GetMessage(ctx context.Context, uid uint32) (*Message, error) {
	// Need to fetch by UID
	uidSet := imap.UIDSet{}
	uidSet.AddNum(imap.UID(uid))

	fetchOptions := &imap.FetchOptions{
		Envelope:    true,
		Flags:       true,
		UID:         true,
		BodySection: []*imap.FetchItemBodySection{{Peek: true}},
	}

	fetchCmd := c.client.Fetch(uidSet, fetchOptions)
	defer fetchCmd.Close()

	msg := fetchCmd.Next()
	if msg == nil {
		return nil, fmt.Errorf("message not found")
	}

	var result Message
	result.UID = uid

	for {
		item := msg.Next()
		if item == nil {
			break
		}
		switch data := item.(type) {
		case imapclient.FetchItemDataEnvelope:
			result.Subject = data.Envelope.Subject
			result.Date = data.Envelope.Date
			if len(data.Envelope.From) > 0 {
				result.From = data.Envelope.From[0].Addr()
			}
			for _, addr := range data.Envelope.To {
				result.To = append(result.To, addr.Addr())
			}
		case imapclient.FetchItemDataFlags:
			for _, f := range data.Flags {
				if f == imap.FlagSeen {
					result.Seen = true
					break
				}
			}
		case imapclient.FetchItemDataBodySection:
			body, err := io.ReadAll(data.Literal)
			if err == nil {
				result.Body = string(body)
			}
		}
	}

	if err := fetchCmd.Close(); err != nil {
		return nil, fmt.Errorf("fetch failed: %w", err)
	}

	return &result, nil
}

func (c *imapConnection) MarkRead(ctx context.Context, uid uint32) error {
	uidSet := imap.UIDSet{}
	uidSet.AddNum(imap.UID(uid))

	storeCmd := c.client.Store(uidSet, &imap.StoreFlags{
		Op:    imap.StoreFlagsAdd,
		Flags: []imap.Flag{imap.FlagSeen},
	}, nil)

	if err := storeCmd.Close(); err != nil {
		return fmt.Errorf("mark read failed: %w", err)
	}
	return nil
}

func (c *imapConnection) MoveMessage(ctx context.Context, uid uint32, destFolder string) error {
	uidSet := imap.UIDSet{}
	uidSet.AddNum(imap.UID(uid))

	// Try MOVE command first (if server supports it)
	moveCmd := c.client.Move(uidSet, destFolder)
	if _, err := moveCmd.Wait(); err != nil {
		// Fall back to COPY + DELETE
		copyCmd := c.client.Copy(uidSet, destFolder)
		if _, err := copyCmd.Wait(); err != nil {
			return fmt.Errorf("copy failed: %w", err)
		}

		// Mark as deleted
		storeCmd := c.client.Store(uidSet, &imap.StoreFlags{
			Op:    imap.StoreFlagsAdd,
			Flags: []imap.Flag{imap.FlagDeleted},
		}, nil)
		if err := storeCmd.Close(); err != nil {
			return fmt.Errorf("delete flag failed: %w", err)
		}

		// Expunge
		expungeCmd := c.client.Expunge()
		if err := expungeCmd.Close(); err != nil {
			return fmt.Errorf("expunge failed: %w", err)
		}
	}
	return nil
}

package imap

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/wardgate/wardgate/internal/policy"
)

// PoolGetter is the interface for getting connections from the pool.
type PoolGetter interface {
	Get(ctx context.Context, endpoint string, cfg ConnectionConfig) (Connection, error)
	Put(endpoint string, conn Connection)
}

// HandlerConfig configures the IMAP handler.
type HandlerConfig struct {
	EndpointName     string
	ConnectionConfig ConnectionConfig
}

// Handler handles REST requests for IMAP operations.
type Handler struct {
	pool     PoolGetter
	engine   *policy.Engine
	config   HandlerConfig
}

// NewHandler creates a new IMAP REST handler.
func NewHandler(pool PoolGetter, engine *policy.Engine, cfg HandlerConfig) *Handler {
	return &Handler{
		pool:   pool,
		engine: engine,
		config: cfg,
	}
}

// ServeHTTP handles incoming REST requests and routes them to IMAP operations.
func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// Get rate limit key from header
	agentID := r.Header.Get("X-Agent-ID")
	if agentID == "" {
		agentID = r.RemoteAddr
	}

	// Evaluate policy
	decision := h.engine.EvaluateWithKey(r.Method, r.URL.Path, agentID)
	if decision.Action == policy.Deny {
		http.Error(w, decision.Message, http.StatusForbidden)
		return
	}
	if decision.Action == policy.RateLimited {
		w.Header().Set("Retry-After", "60")
		http.Error(w, decision.Message, http.StatusTooManyRequests)
		return
	}

	// Get connection from pool
	conn, err := h.pool.Get(r.Context(), h.config.EndpointName, h.config.ConnectionConfig)
	if err != nil {
		// Log the actual error for debugging
		fmt.Fprintf(os.Stderr, "IMAP connection error for %s: %v\n", h.config.EndpointName, err)
		http.Error(w, "failed to connect to IMAP server", http.StatusBadGateway)
		return
	}
	defer h.pool.Put(h.config.EndpointName, conn)

	// Route request
	// API structure:
	//   /folders                               - list folders
	//   /folders/{folder}                      - list messages in folder
	//   /folders/{folder}/messages/{uid}       - get message
	//   /folders/{folder}/messages/{uid}/mark-read
	//   /folders/{folder}/messages/{uid}/move?to={dest}
	path := strings.TrimPrefix(r.URL.Path, "/")
	parts := strings.Split(path, "/")

	// Find "messages" index if present
	messagesIdx := -1
	for i, p := range parts {
		if p == "messages" {
			messagesIdx = i
			break
		}
	}

	switch {
	case path == "folders" && r.Method == "GET":
		h.handleListFolders(w, r, conn)

	case len(parts) >= 2 && parts[0] == "folders" && messagesIdx == -1 && r.Method == "GET":
		// /folders/{folder} - no "messages" in path, so it's listing folder contents
		folder := decodeFolder(strings.Join(parts[1:], "/"))
		if folder == "" {
			http.Error(w, "invalid folder name", http.StatusBadRequest)
			return
		}
		h.handleFetchMessages(w, r, conn, folder)

	case parts[0] == "folders" && messagesIdx > 1 && len(parts) == messagesIdx+2 && r.Method == "GET":
		// /folders/{folder}/messages/{uid}
		folder := decodeFolder(strings.Join(parts[1:messagesIdx], "/"))
		if folder == "" {
			http.Error(w, "invalid folder name", http.StatusBadRequest)
			return
		}
		h.handleGetMessage(w, r, conn, folder, parts[messagesIdx+1])

	case parts[0] == "folders" && messagesIdx > 1 && len(parts) == messagesIdx+3 && parts[len(parts)-1] == "mark-read" && r.Method == "POST":
		// /folders/{folder}/messages/{uid}/mark-read
		folder := decodeFolder(strings.Join(parts[1:messagesIdx], "/"))
		if folder == "" {
			http.Error(w, "invalid folder name", http.StatusBadRequest)
			return
		}
		h.handleMarkRead(w, r, conn, folder, parts[messagesIdx+1])

	case parts[0] == "folders" && messagesIdx > 1 && len(parts) == messagesIdx+3 && parts[len(parts)-1] == "move" && r.Method == "POST":
		// /folders/{folder}/messages/{uid}/move?to={dest}
		folder := decodeFolder(strings.Join(parts[1:messagesIdx], "/"))
		if folder == "" {
			http.Error(w, "invalid folder name", http.StatusBadRequest)
			return
		}
		h.handleMoveMessage(w, r, conn, folder, parts[messagesIdx+1])

	default:
		http.Error(w, "not found", http.StatusNotFound)
	}
}

func (h *Handler) handleListFolders(w http.ResponseWriter, r *http.Request, conn Connection) {
	folders, err := conn.ListFolders(r.Context())
	if err != nil {
		http.Error(w, "failed to list folders", http.StatusInternalServerError)
		return
	}
	h.writeJSON(w, folders)
}

func (h *Handler) handleFetchMessages(w http.ResponseWriter, r *http.Request, conn Connection, folder string) {
	opts := FetchOptions{
		Folder: folder,
	}

	// Parse query params
	if limit := r.URL.Query().Get("limit"); limit != "" {
		if n, err := strconv.Atoi(limit); err == nil {
			opts.Limit = n
		}
	}

	if since := r.URL.Query().Get("since"); since != "" {
		if t, err := time.Parse("2006-01-02", since); err == nil {
			opts.Since = &t
		}
	}

	if before := r.URL.Query().Get("before"); before != "" {
		if t, err := time.Parse("2006-01-02", before); err == nil {
			opts.Before = &t
		}
	}

	messages, err := conn.FetchMessages(r.Context(), opts)
	if err != nil {
		http.Error(w, "failed to fetch messages", http.StatusInternalServerError)
		return
	}
	h.writeJSON(w, messages)
}

func (h *Handler) handleGetMessage(w http.ResponseWriter, r *http.Request, conn Connection, folder, uidStr string) {
	uid, err := strconv.ParseUint(uidStr, 10, 32)
	if err != nil {
		http.Error(w, "invalid message UID", http.StatusBadRequest)
		return
	}

	// Select folder first to ensure UID is from this folder
	if _, err := conn.SelectFolder(r.Context(), folder); err != nil {
		fmt.Fprintf(os.Stderr, "IMAP SelectFolder error for '%s': %v\n", folder, err)
		http.Error(w, "failed to select folder", http.StatusInternalServerError)
		return
	}

	msg, err := conn.GetMessage(r.Context(), uint32(uid))
	if err != nil {
		fmt.Fprintf(os.Stderr, "IMAP GetMessage error for UID %d in '%s': %v\n", uid, folder, err)
		http.Error(w, "failed to get message", http.StatusInternalServerError)
		return
	}
	h.writeJSON(w, msg)
}

func (h *Handler) handleMarkRead(w http.ResponseWriter, r *http.Request, conn Connection, folder, uidStr string) {
	uid, err := strconv.ParseUint(uidStr, 10, 32)
	if err != nil {
		http.Error(w, "invalid message UID", http.StatusBadRequest)
		return
	}

	// Select folder first to ensure UID is from this folder
	if _, err := conn.SelectFolder(r.Context(), folder); err != nil {
		fmt.Fprintf(os.Stderr, "IMAP SelectFolder error for '%s': %v\n", folder, err)
		http.Error(w, "failed to select folder", http.StatusInternalServerError)
		return
	}

	if err := conn.MarkRead(r.Context(), uint32(uid)); err != nil {
		fmt.Fprintf(os.Stderr, "IMAP MarkRead error for UID %d: %v\n", uid, err)
		http.Error(w, "failed to mark message as read", http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusOK)
	h.writeJSON(w, map[string]string{"status": "ok"})
}

func (h *Handler) handleMoveMessage(w http.ResponseWriter, r *http.Request, conn Connection, folder, uidStr string) {
	uid, err := strconv.ParseUint(uidStr, 10, 32)
	if err != nil {
		http.Error(w, "invalid message UID", http.StatusBadRequest)
		return
	}

	destFolder := r.URL.Query().Get("to")
	if destFolder == "" {
		http.Error(w, "missing 'to' parameter", http.StatusBadRequest)
		return
	}

	// Select source folder first to ensure UID is from this folder
	if _, err := conn.SelectFolder(r.Context(), folder); err != nil {
		fmt.Fprintf(os.Stderr, "IMAP SelectFolder error for '%s': %v\n", folder, err)
		http.Error(w, "failed to select folder", http.StatusInternalServerError)
		return
	}

	if err := conn.MoveMessage(r.Context(), uint32(uid), destFolder); err != nil {
		fmt.Fprintf(os.Stderr, "IMAP MoveMessage error for UID %d to '%s': %v\n", uid, destFolder, err)
		http.Error(w, "failed to move message", http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusOK)
	h.writeJSON(w, map[string]string{"status": "ok"})
}

func (h *Handler) writeJSON(w http.ResponseWriter, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(v)
}

// decodeFolder URL-decodes a folder name. Returns empty string on error.
func decodeFolder(encoded string) string {
	decoded, err := url.PathUnescape(encoded)
	if err != nil {
		return ""
	}
	return decoded
}

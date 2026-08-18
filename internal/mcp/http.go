package mcp

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"
)

// The APK's streamable MCP endpoint uses this header and exposes it to browser
// clients through CORS.
const streamSessionHeader = "Mcp-Session-Id"
const maxStreamableBody int64 = 1 << 20

type streamSessionStore struct {
	mu       sync.Mutex
	sessions map[string]time.Time
}

var globalStreamSessions = &streamSessionStore{sessions: map[string]time.Time{}}

// HandleStreamable implements the Streamable HTTP JSON-RPC transport in
// addition to the legacy SSE transport in server.go. The APK accepts GET,
// POST, and DELETE, exposes the MCP session header, and dispatches requests to
// the same handleRPC implementation as its SSE endpoint.
func HandleStreamable(w http.ResponseWriter, r *http.Request) {
	setStreamableHeaders(w)

	switch r.Method {
	case http.MethodOptions:
		w.WriteHeader(http.StatusNoContent)
		return
	case http.MethodGet:
		streamNoop(w, r)
		return
	case http.MethodDelete:
		sessionID := strings.TrimSpace(r.Header.Get(streamSessionHeader))
		if sessionID != "" {
			globalStreamSessions.drop(sessionID)
		}
		w.WriteHeader(http.StatusNoContent)
		return
	case http.MethodPost:
		// handled below
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	body, err := readAllLimited(r.Body, maxStreamableBody)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, newRPCError(nil, -32700, "parse error: "+err.Error()))
		return
	}

	requests, isBatch, err := decodeStreamableRequests(body)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, newRPCError(nil, -32700, "parse error: "+err.Error()))
		return
	}
	if len(requests) == 0 {
		writeJSON(w, http.StatusBadRequest, newRPCError(nil, -32600, "invalid request"))
		return
	}

	// The APK calls streamSessionStore.ensure after parsing the body. An absent
	// header creates a fresh session and exposes it in the response header.
	sessionID := strings.TrimSpace(r.Header.Get(streamSessionHeader))
	resolvedID, sess := globalStreamSessions.ensure(sessionID)
	if resolvedID != sessionID {
		w.Header().Set(streamSessionHeader, resolvedID)
	}
	if sess == nil {
		writeJSON(w, http.StatusNotFound, newRPCError(nil, -32000, "session not found"))
		return
	}
	globalStreamSessions.touch(resolvedID)

	responses := make([]*jsonRPCResponse, 0, len(requests))
	for i := range requests {
		if response := handleRPC(r.Context(), sess, &requests[i]); response != nil {
			responses = append(responses, response)
		}
	}
	if len(responses) == 0 {
		w.WriteHeader(http.StatusAccepted)
		return
	}
	if isBatch {
		writeJSON(w, http.StatusOK, responses)
		return
	}
	writeJSON(w, http.StatusOK, responses[0])
}

func decodeStreamableRequests(body []byte) ([]jsonRPCRequest, bool, error) {
	trimmed := strings.TrimSpace(string(body))
	if strings.HasPrefix(trimmed, "[") {
		var requests []jsonRPCRequest
		if err := json.Unmarshal([]byte(trimmed), &requests); err != nil {
			return nil, true, err
		}
		return requests, true, nil
	}
	var request jsonRPCRequest
	if err := json.Unmarshal([]byte(trimmed), &request); err != nil {
		return nil, false, err
	}
	return []jsonRPCRequest{request}, false, nil
}

func streamNoop(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	_, _ = io.WriteString(w, ": streamable-mcp\n\n")
	if f, ok := w.(http.Flusher); ok {
		f.Flush()
	}
}

func setStreamableHeaders(w http.ResponseWriter) {
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization, Mcp-Session-Id, MCP-Protocol-Version")
	w.Header().Set("Access-Control-Expose-Headers", streamSessionHeader)
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func readAllLimited(body io.ReadCloser, limit int64) ([]byte, error) {
	defer body.Close()
	data, err := io.ReadAll(io.LimitReader(body, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > limit {
		return nil, fmt.Errorf("request body exceeds %d bytes", limit)
	}
	return data, nil
}

func (s *streamSessionStore) ensure(existing string) (string, *session) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if existing != "" {
		if sess := GlobalRegistry.getSession(existing); sess != nil {
			s.sessions[existing] = time.Now()
			return existing, sess
		}
	}
	id := GlobalRegistry.RegisterSession(nil)
	s.sessions[id] = time.Now()
	return id, GlobalRegistry.getSession(id)
}

func (s *streamSessionStore) touch(id string) {
	s.mu.Lock()
	if _, ok := s.sessions[id]; ok {
		s.sessions[id] = time.Now()
	}
	s.mu.Unlock()
}

func (s *streamSessionStore) drop(id string) {
	s.mu.Lock()
	delete(s.sessions, id)
	s.mu.Unlock()
	GlobalRegistry.UnregisterSession(id)
}

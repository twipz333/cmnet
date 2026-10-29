package web

import (
	"encoding/json"
	"net/http"
	"sync"
)

// hub fans state updates out to every connected browser over Server-Sent Events.
// SSE (not WebSocket) because it is one-way, survives reconnects on its own, and
// needs no dependency beyond net/http.
type hub struct {
	mu   sync.Mutex
	next int
	subs map[int]chan []byte
}

func newHub() *hub {
	return &hub{subs: map[int]chan []byte{}}
}

func (h *hub) subscribe() (id int, ch chan []byte) {
	h.mu.Lock()
	defer h.mu.Unlock()
	id = h.next
	h.next++
	// Buffered so a slow client cannot block a draft transition; when the buffer
	// fills we drop the update — the next one carries the full state anyway.
	ch = make(chan []byte, 8)
	h.subs[id] = ch
	return id, ch
}

func (h *hub) unsubscribe(id int) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if ch, ok := h.subs[id]; ok {
		delete(h.subs, id)
		close(ch)
	}
}

func (h *hub) publish(payload []byte) {
	h.mu.Lock()
	defer h.mu.Unlock()
	for _, ch := range h.subs {
		select {
		case ch <- payload:
		default: // slow client: skip this update, the next full state supersedes it
		}
	}
}

// broadcast pushes the current state to all listeners. ring marks a state where an
// objective just entered its alert window, so browsers can sound it once.
func (s *Server) broadcast(ring bool) {
	s.publishLive() // following guests see the same board
	payload, err := json.Marshal(s.snapshot(ring))
	if err != nil {
		return
	}
	s.hub.publish(payload)
}

// handleEvents streams state updates as SSE. The first message is the current
// state, so a freshly opened tab renders immediately without a separate fetch.
func (s *Server) handleEvents(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming unsupported", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Connection", "keep-alive")
	w.WriteHeader(http.StatusOK)

	id, ch := s.hub.subscribe()
	defer s.hub.unsubscribe(id)

	if payload, err := json.Marshal(s.snapshot(false)); err == nil {
		writeSSE(w, payload)
		flusher.Flush()
	}

	for {
		select {
		case <-r.Context().Done():
			return
		case payload, open := <-ch:
			if !open {
				return
			}
			writeSSE(w, payload)
			flusher.Flush()
		}
	}
}

// writeSSE emits one SSE data frame. The payload is compact JSON from
// json.Marshal, which never contains a raw newline, so a single data: line is safe.
func writeSSE(w http.ResponseWriter, payload []byte) {
	_, _ = w.Write([]byte("data: "))
	_, _ = w.Write(payload)
	_, _ = w.Write([]byte("\n\n"))
}

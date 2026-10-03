package server

import (
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
)

type Hub struct {
	mu      sync.Mutex
	clients map[chan string]bool
}

func NewHub() *Hub {
	return &Hub{clients: make(map[chan string]bool)}
}

func (h *Hub) Add() chan string {
	ch := make(chan string, 16)
	h.mu.Lock()
	h.clients[ch] = true
	h.mu.Unlock()
	return ch
}

func (h *Hub) Remove(ch chan string) {
	h.mu.Lock()
	delete(h.clients, ch)
	h.mu.Unlock()
	close(ch)
}

func (h *Hub) Broadcast(event string) {
	h.mu.Lock()
	for ch := range h.clients {
		select {
		case ch <- event:
		default:
		}
	}
	h.mu.Unlock()
}

func (h *Hub) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming unsupported", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	ch := h.Add()
	defer h.Remove(ch)

	fmt.Fprintf(w, "data: connected\n\n")
	flusher.Flush()

	for {
		select {
		case event := <-ch:
			fmt.Fprintf(w, "data: %s\n\n", event)
			flusher.Flush()
		case <-r.Context().Done():
			return
		}
	}
}

func BroadcastFileChange(hub *Hub, path string) {
	event, _ := json.Marshal(map[string]string{"type": "change", "path": path})
	hub.Broadcast(string(event))
}

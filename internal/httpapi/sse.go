package httpapi

import (
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/geoffo-dev/workflow-management/internal/apperr"
	"github.com/geoffo-dev/workflow-management/internal/identity"
)

// streamEvents is GET /api/v1/events?project=KEY: server-sent events for live boards.
func (s *Server) streamEvents(w http.ResponseWriter, r *http.Request) {
	a := identity.ActorFrom(r.Context())
	if a == nil {
		WriteProblem(s.Log, w, r, apperr.Unauthorized())
		return
	}
	p, _, err := s.Tracker.ProjectFor(r.Context(), a, r.URL.Query().Get("project"), "")
	if err != nil {
		WriteProblem(s.Log, w, r, err)
		return
	}
	flusher, ok := w.(http.Flusher)
	if !ok {
		WriteProblem(s.Log, w, r, fmt.Errorf("streaming unsupported"))
		return
	}
	h := w.Header()
	h.Set("Content-Type", "text/event-stream")
	h.Set("Cache-Control", "no-cache")
	h.Set("Connection", "keep-alive")
	h.Set("X-Accel-Buffering", "no") // disable proxy buffering (nginx / Traefik)
	w.WriteHeader(http.StatusOK)
	fmt.Fprintf(w, "retry: 3000\nevent: ready\ndata: {\"project\":%q}\n\n", p.Key)
	flusher.Flush()

	events, cancel := s.Hub.Subscribe(p.ID)
	defer cancel()
	keepAlive := time.NewTicker(25 * time.Second)
	defer keepAlive.Stop()
	for {
		select {
		case <-r.Context().Done():
			return
		case <-keepAlive.C:
			fmt.Fprint(w, ": keep-alive\n\n")
			flusher.Flush()
		case ev, ok := <-events:
			if !ok {
				return
			}
			b, _ := json.Marshal(ev)
			fmt.Fprintf(w, "event: %s\ndata: %s\n\n", ev.Type, b)
			flusher.Flush()
		}
	}
}

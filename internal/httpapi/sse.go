package httpapi

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/geoffo-dev/workflow-management/internal/apperr"
	"github.com/geoffo-dev/workflow-management/internal/identity"
)

// maxStreamProjects caps the projects one event stream follows.
const maxStreamProjects = 500

// streamEvents is GET /api/v1/events?project=KEY (or ?projects=A,B, or ?projects=*):
// server-sent events for live boards, timelines and dashboards.
func (s *Server) streamEvents(w http.ResponseWriter, r *http.Request) {
	a := identity.ActorFrom(r.Context())
	if a == nil {
		WriteProblem(s.Log, w, r, apperr.Unauthorized())
		return
	}
	// ?project=KEY for one project, ?projects=A,B for several, ?projects=* for every project
	// the caller can browse (as of connecting).
	q := r.URL.Query()
	var keys []string
	switch {
	case q.Get("projects") == "*":
	case q.Get("projects") != "":
		keys = strings.Split(q.Get("projects"), ",")
	default:
		keys = []string{q.Get("project")}
	}
	if len(keys) > maxStreamProjects {
		WriteProblem(s.Log, w, r, apperr.Invalid("projects", "at most %d projects per stream", maxStreamProjects))
		return
	}
	projects, err := s.Tracker.ProjectsByKeys(r.Context(), a, keys)
	if err != nil {
		WriteProblem(s.Log, w, r, err)
		return
	}
	if len(projects) > maxStreamProjects {
		projects = projects[:maxStreamProjects]
	}
	ids := make([]uuid.UUID, len(projects))
	names := make([]string, len(projects))
	for i, p := range projects {
		ids[i], names[i] = p.ID, p.Key
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
	ready, _ := json.Marshal(map[string]any{"projects": names})
	fmt.Fprintf(w, "retry: 3000\nevent: ready\ndata: %s\n\n", ready)
	flusher.Flush()

	events, cancel := s.Hub.Subscribe(ids...)
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

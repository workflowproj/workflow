// Package events carries live updates to open boards and issue views. Mutations call Publish
// inside their transaction (pg_notify is delivered on commit), web pods LISTEN and fan events
// out to server-sent-event subscribers.
package events

import (
	"context"
	"encoding/json"
	"log/slog"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

const Channel = "wfm_events"

type Event struct {
	Type      string     `json:"type"`
	ProjectID uuid.UUID  `json:"projectId"`
	IssueID   *uuid.UUID `json:"issueId,omitempty"`
	IssueKey  string     `json:"issueKey,omitempty"`
	ActorID   *uuid.UUID `json:"actorId,omitempty"`
}

// Publish queues a notification on the transaction; it is sent only if the transaction commits.
func Publish(ctx context.Context, tx pgx.Tx, ev Event) error {
	b, err := json.Marshal(ev)
	if err != nil {
		return err
	}
	_, err = tx.Exec(ctx, "SELECT pg_notify($1, $2)", Channel, string(b))
	return err
}

var subscribers = promauto.NewGauge(prometheus.GaugeOpts{Name: "wfm_sse_subscribers", Help: "Open server-sent-event streams."})

// Hub listens on the Postgres channel and fans events out to in-process subscribers.
type Hub struct {
	pool *pgxpool.Pool
	log  *slog.Logger
	// ListenURL, when set, is a direct database address for the LISTEN session. It is needed
	// when the pool goes through a transaction-mode pooler, where LISTEN does not work.
	ListenURL string

	mu     sync.RWMutex
	subs   map[uuid.UUID]map[chan Event]struct{}
	closed bool
}

func NewHub(pool *pgxpool.Pool, log *slog.Logger) *Hub {
	return &Hub{pool: pool, log: log, subs: map[uuid.UUID]map[chan Event]struct{}{}}
}

// Subscribe returns one channel of events for the given projects and a function to unsubscribe.
func (h *Hub) Subscribe(projectIDs ...uuid.UUID) (<-chan Event, func()) {
	ch := make(chan Event, 64)
	h.mu.Lock()
	if h.closed {
		h.mu.Unlock()
		close(ch)
		subscribers.Inc()
		return ch, func() { subscribers.Dec() }
	}
	for _, id := range projectIDs {
		if h.subs[id] == nil {
			h.subs[id] = map[chan Event]struct{}{}
		}
		h.subs[id][ch] = struct{}{}
	}
	h.mu.Unlock()
	subscribers.Inc()
	var once sync.Once
	return ch, func() {
		once.Do(func() {
			h.mu.Lock()
			for _, id := range projectIDs {
				delete(h.subs[id], ch)
				if len(h.subs[id]) == 0 {
					delete(h.subs, id)
				}
			}
			h.mu.Unlock()
			subscribers.Dec()
		})
	}
}

// Close ends every open stream (their channels close) so a shutting-down server does not wait
// for long-lived SSE connections; clients reconnect to another replica.
func (h *Hub) Close() {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.closed {
		return
	}
	h.closed = true
	done := map[chan Event]bool{} // a stream may be registered under several projects
	for _, chans := range h.subs {
		for ch := range chans {
			if !done[ch] {
				done[ch] = true
				close(ch)
			}
		}
	}
	h.subs = map[uuid.UUID]map[chan Event]struct{}{}
}

func (h *Hub) dispatch(ev Event) {
	h.mu.RLock()
	defer h.mu.RUnlock()
	for ch := range h.subs[ev.ProjectID] {
		select {
		case ch <- ev:
		default: // a slow client misses an event; it refetches on the next one
		}
	}
}

// Run listens until ctx is cancelled, reconnecting with backoff.
func (h *Hub) Run(ctx context.Context) {
	backoff := time.Second
	for ctx.Err() == nil {
		err := h.listen(ctx)
		if ctx.Err() != nil {
			return
		}
		h.log.Warn("event listener disconnected; retrying", "error", err, "in", backoff)
		select {
		case <-ctx.Done():
			return
		case <-time.After(backoff):
		}
		backoff = min(backoff*2, 30*time.Second)
	}
}

func (h *Hub) listen(ctx context.Context) error {
	conn, release, err := h.listenConn(ctx)
	if err != nil {
		return err
	}
	defer release()
	if _, err := conn.Exec(ctx, "LISTEN "+Channel); err != nil {
		return err
	}
	for {
		n, err := conn.WaitForNotification(ctx)
		if err != nil {
			return err
		}
		var ev Event
		if err := json.Unmarshal([]byte(n.Payload), &ev); err != nil {
			continue
		}
		h.dispatch(ev)
	}
}

// listenConn returns a session that can LISTEN: a dedicated direct connection when ListenURL
// is set, otherwise one held from the pool.
func (h *Hub) listenConn(ctx context.Context) (*pgx.Conn, func(), error) {
	if h.ListenURL != "" {
		conn, err := pgx.Connect(ctx, h.ListenURL)
		if err != nil {
			return nil, nil, err
		}
		return conn, func() {
			closeCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			_ = conn.Close(closeCtx)
		}, nil
	}
	pc, err := h.pool.Acquire(ctx)
	if err != nil {
		return nil, nil, err
	}
	return pc.Conn(), pc.Release, nil
}

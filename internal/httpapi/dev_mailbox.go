package httpapi

import (
	"context"
	"encoding/json"
	"slices"
	"strings"
	"time"

	"github.com/geoffo-dev/workflow-management/internal/jobs"
	"github.com/geoffo-dev/workflow-management/internal/mail"
)

// devMessages lists the development mailbox: what this process's in-memory sender holds plus
// queued emails other processes sent (completed email_send jobs). With web and worker in
// separate processes, the worker sends queued mail from its own memory, out of the web
// process's sight.
func (s *Server) devMessages(ctx context.Context, box *mail.Memory, to string) []mail.Sent {
	out := box.Messages(to)
	rows, err := s.Pool.Query(ctx, `SELECT args, coalesce(finalized_at, created_at) FROM river_job WHERE kind = $1 AND state = 'completed' ORDER BY id`, jobs.SendEmail{}.Kind())
	if err != nil {
		s.Log.Warn("dev mailbox", "error", err)
		return out
	}
	defer rows.Close()
	seen := map[string]bool{}
	key := func(m mail.Message) string { return strings.Join(m.To, ",") + "\x00" + m.Subject + "\x00" + m.Text }
	for _, m := range out {
		seen[key(m.Message)] = true
	}
	for rows.Next() {
		var raw []byte
		var at time.Time
		var j jobs.SendEmail
		if rows.Scan(&raw, &at) != nil || json.Unmarshal(raw, &j) != nil {
			continue
		}
		m := mail.FromJob(j)
		if seen[key(m)] || (to != "" && !slices.ContainsFunc(m.To, func(a string) bool { return strings.EqualFold(a, to) })) {
			continue
		}
		seen[key(m)] = true
		out = append(out, mail.Sent{Message: m, At: at})
	}
	slices.SortStableFunc(out, func(a, b mail.Sent) int { return a.At.Compare(b.At) })
	return out
}

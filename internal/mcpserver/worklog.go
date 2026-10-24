package mcpserver

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/geoffo-dev/workflow-management/internal/api"
	"github.com/geoffo-dev/workflow-management/internal/timetrack"
)

// units is the server's working day and week, which durations such as "1d" use.
func (t *tools) units(ctx context.Context) timetrack.Units {
	s, err := as[api.GetTimeTrackingSettings200JSONResponse](t.api.GetTimeTrackingSettings(ctx, api.GetTimeTrackingSettingsRequestObject{}))
	if err != nil || s.HoursPerDay <= 0 || s.DaysPerWeek <= 0 {
		return timetrack.Default
	}
	return timetrack.Units{HoursPerDay: s.HoursPerDay, DaysPerWeek: s.DaysPerWeek}
}

// timeTrackingText is "estimated 1d · logged 3h · remaining 5h", or "" when nothing is tracked.
func (t *tools) timeTrackingText(ctx context.Context, tt *api.TimeTracking) string {
	if tt == nil || (tt.OriginalSeconds == nil && tt.RemainingSeconds == nil && tt.SpentSeconds == 0) {
		return ""
	}
	u := t.units(ctx)
	var parts []string
	if tt.OriginalSeconds != nil {
		parts = append(parts, "estimated "+timetrack.Format(*tt.OriginalSeconds, u))
	}
	parts = append(parts, "logged "+timetrack.Format(tt.SpentSeconds, u))
	if tt.RemainingSeconds != nil {
		parts = append(parts, "remaining "+timetrack.Format(*tt.RemainingSeconds, u))
	}
	text := strings.Join(parts, " · ")
	if tt.OriginalSeconds != nil && tt.SpentSeconds > *tt.OriginalSeconds {
		text += fmt.Sprintf(" (over the estimate by %s)", timetrack.Format(tt.SpentSeconds-*tt.OriginalSeconds, u))
	}
	return text
}

type logWorkIn struct {
	Key               string `json:"key" jsonschema:"issue key"`
	TimeSpent         string `json:"timeSpent" jsonschema:"time spent, e.g. 45m, 2h, 1h 30m or 1d (a bare number is minutes)"`
	Started           string `json:"started,omitempty" jsonschema:"when the work was done: YYYY-MM-DD, YYYY-MM-DD HH:MM (UTC) or RFC 3339 (default now)"`
	Comment           string `json:"comment,omitempty" jsonschema:"what was done"`
	RemainingEstimate string `json:"remainingEstimate,omitempty" jsonschema:"set the remaining estimate to this, e.g. 4h (by default it goes down by the time logged)"`
	LeaveEstimate     bool   `json:"leaveEstimate,omitempty" jsonschema:"keep the remaining estimate as it is"`
}

// startedAt reads a work date; a bare date means midday UTC so it stays that day in most zones.
func startedAt(s string) (*time.Time, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil, nil
	}
	if d, err := time.Parse(time.DateOnly, s); err == nil {
		d = d.Add(12 * time.Hour)
		return &d, nil
	}
	for _, layout := range []string{"2006-01-02 15:04", time.RFC3339} {
		if d, err := time.Parse(layout, s); err == nil {
			return &d, nil
		}
	}
	return nil, fmt.Errorf("started must be YYYY-MM-DD, YYYY-MM-DD HH:MM or RFC 3339")
}

func (t *tools) logWork(ctx context.Context, _ *mcp.CallToolRequest, in logWorkIn) (*mcp.CallToolResult, any, error) {
	ctx, err := t.ctx(ctx, true)
	if err != nil {
		return nil, nil, err
	}
	u := t.units(ctx)
	spent, err := timetrack.Parse(in.TimeSpent, u)
	if err != nil {
		return nil, nil, fmt.Errorf("timeSpent: %v", err)
	}
	body := api.WorklogInput{TimeSpentSeconds: spent}
	if body.StartedAt, err = startedAt(in.Started); err != nil {
		return nil, nil, err
	}
	if c := strings.TrimSpace(in.Comment); c != "" {
		body.Comment = &c
	}
	if in.RemainingEstimate != "" {
		r, err := timetrack.Parse(in.RemainingEstimate, u)
		if err != nil {
			return nil, nil, fmt.Errorf("remainingEstimate: %v", err)
		}
		body.RemainingEstimateSeconds = &r
	} else if in.LeaveEstimate {
		leave := api.AdjustEstimateLeave
		body.AdjustEstimate = &leave
	}
	w, err := as[api.AddWorklog201JSONResponse](t.api.AddWorklog(ctx, api.AddWorklogRequestObject{IssueKey: in.Key, Body: &body}))
	if err != nil {
		return nil, nil, err
	}
	msg := fmt.Sprintf("Logged %s on %s (work started %s UTC).", timetrack.Format(w.TimeSpentSeconds, u), w.IssueKey, w.StartedAt.UTC().Format("2006-01-02 15:04"))
	if i, err := as[api.GetIssue200JSONResponse](t.api.GetIssue(ctx, api.GetIssueRequestObject{IssueKey: w.IssueKey})); err == nil {
		if tt := t.timeTrackingText(ctx, i.TimeTracking); tt != "" {
			msg += "\nTime tracking: " + tt + "."
		}
	}
	return text(msg), nil, nil
}

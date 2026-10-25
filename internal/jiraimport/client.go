package jiraimport

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// Flavors of Jira: Cloud speaks REST v3 (ADF rich text, account ids, basic auth with an API
// token); Data Center / Server speaks REST v2 (wiki markup, user keys, a personal access token).
const (
	Cloud      = "cloud"
	DataCenter = "datacenter"
)

// Client is a small Jira REST client. Every call honours rate limits (429 with Retry-After) and
// retries transient failures with exponential backoff; pagination is left to the callers, which
// fetch one page at a time so memory stays bounded.
type Client struct {
	Flavor  string
	BaseURL string // without a trailing slash
	Email   string // Cloud only
	Token   string
	HTTP    *http.Client
	// Download is used for attachment content (longer timeout); defaults to HTTP.
	Download *http.Client
	// Retries is how many times a rate-limited or failed request is retried.
	Retries int
	// Sleep waits between attempts; tests replace it.
	Sleep func(ctx context.Context, d time.Duration) error
	// Requests counts HTTP requests made (for the report).
	Requests int
}

// APIError is a non-2xx answer from Jira.
type APIError struct {
	Status  int
	Method  string
	Path    string
	Message string
}

func (e *APIError) Error() string {
	switch e.Status {
	case http.StatusUnauthorized:
		return "Jira rejected the credentials (401): check the email and API token, or the personal access token"
	case http.StatusForbidden:
		return fmt.Sprintf("Jira refused access to %s (403): the account needs browse permission%s", e.Path, suffix(e.Message))
	case http.StatusNotFound:
		return fmt.Sprintf("not found in Jira: %s%s", e.Path, suffix(e.Message))
	}
	return fmt.Sprintf("Jira %s %s: HTTP %d%s", e.Method, e.Path, e.Status, suffix(e.Message))
}

func suffix(msg string) string {
	if msg == "" {
		return ""
	}
	return ": " + msg
}

// IsNotFound reports a 404 (or a 403 for an optional API such as Jira Software's agile API).
func IsNotFound(err error) bool {
	var e *APIError
	return errors.As(err, &e) && e.Status == http.StatusNotFound
}

func isStatus(err error, codes ...int) bool {
	var e *APIError
	if !errors.As(err, &e) {
		return false
	}
	for _, c := range codes {
		if e.Status == c {
			return true
		}
	}
	return false
}

func (c *Client) api() string {
	if c.Flavor == Cloud {
		return "/rest/api/3"
	}
	return "/rest/api/2"
}

func (c *Client) retries() int {
	if c.Retries > 0 {
		return c.Retries
	}
	return 6
}

func (c *Client) sleep(ctx context.Context, d time.Duration) error {
	if c.Sleep != nil {
		return c.Sleep(ctx, d)
	}
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}

func (c *Client) authorize(req *http.Request) {
	if c.Flavor == Cloud {
		req.Header.Set("Authorization", "Basic "+base64.StdEncoding.EncodeToString([]byte(c.Email+":"+c.Token)))
	} else {
		req.Header.Set("Authorization", "Bearer "+c.Token)
	}
	req.Header.Set("Accept", "application/json")
	// Jira's XSRF check blocks some non-browser requests without this header.
	req.Header.Set("X-Atlassian-Token", "no-check")
	req.Header.Set("User-Agent", "wfm-jira-import")
}

// retryAfter reads Retry-After (seconds or an HTTP date); 0 when absent.
func retryAfter(h http.Header, now time.Time) time.Duration {
	v := strings.TrimSpace(h.Get("Retry-After"))
	if v == "" {
		return 0
	}
	if n, err := strconv.Atoi(v); err == nil {
		return time.Duration(max(n, 0)) * time.Second
	}
	if t, err := http.ParseTime(v); err == nil {
		return max(t.Sub(now), 0)
	}
	return 0
}

// backoff is 1s, 2s, 4s ... capped at a minute.
func backoff(attempt int) time.Duration {
	return time.Duration(math.Min(math.Pow(2, float64(attempt)), 60)) * time.Second
}

// send performs a request with retries. The caller closes the body of a successful response.
func (c *Client) send(ctx context.Context, hc *http.Client, method, rawURL string, body []byte) (*http.Response, error) {
	if hc == nil {
		hc = http.DefaultClient
	}
	var lastErr error
	for attempt := 0; ; attempt++ {
		var rdr io.Reader
		if body != nil {
			rdr = bytes.NewReader(body)
		}
		req, err := http.NewRequestWithContext(ctx, method, rawURL, rdr)
		if err != nil {
			return nil, err
		}
		c.authorize(req)
		if body != nil {
			req.Header.Set("Content-Type", "application/json")
		}
		c.Requests++
		res, err := hc.Do(req)
		wait := time.Duration(0)
		switch {
		case err != nil:
			if ctx.Err() != nil {
				return nil, ctx.Err()
			}
			lastErr = err
			wait = backoff(attempt)
		case res.StatusCode == http.StatusTooManyRequests || res.StatusCode == http.StatusServiceUnavailable:
			lastErr = c.apiError(res, method, rawURL)
			wait = retryAfter(res.Header, time.Now())
			if wait == 0 {
				wait = backoff(attempt)
			}
			// Atlassian asks clients to add jitter-free backoff on top of Retry-After for repeats.
			wait = min(wait+time.Duration(attempt)*time.Second, 5*time.Minute)
		case res.StatusCode >= 500:
			lastErr = c.apiError(res, method, rawURL)
			wait = backoff(attempt)
		case res.StatusCode >= 300:
			return nil, c.apiError(res, method, rawURL)
		default:
			return res, nil
		}
		if attempt >= c.retries() {
			return nil, lastErr
		}
		if err := c.sleep(ctx, wait); err != nil {
			return nil, err
		}
	}
}

func (c *Client) apiError(res *http.Response, method, rawURL string) error {
	defer res.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(res.Body, 8<<10))
	path := rawURL
	if u, err := url.Parse(rawURL); err == nil {
		path = u.Path
	}
	e := &APIError{Status: res.StatusCode, Method: method, Path: path}
	var body struct {
		ErrorMessages []string          `json:"errorMessages"`
		Errors        map[string]string `json:"errors"`
		Message       string            `json:"message"`
	}
	if json.Unmarshal(raw, &body) == nil {
		msgs := body.ErrorMessages
		for k, v := range body.Errors {
			msgs = append(msgs, k+": "+v)
		}
		if body.Message != "" {
			msgs = append(msgs, body.Message)
		}
		e.Message = strings.Join(msgs, "; ")
	}
	return e
}

func (c *Client) url(path string, q url.Values) string {
	u := c.BaseURL + path
	if len(q) > 0 {
		u += "?" + q.Encode()
	}
	return u
}

// get decodes a JSON GET response.
func (c *Client) get(ctx context.Context, path string, q url.Values, out any) error {
	res, err := c.send(ctx, c.HTTP, http.MethodGet, c.url(path, q), nil)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	if err := json.NewDecoder(res.Body).Decode(out); err != nil {
		return fmt.Errorf("decode %s: %w", path, err)
	}
	return nil
}

// post sends JSON and decodes the JSON answer.
func (c *Client) post(ctx context.Context, path string, body, out any) error {
	raw, err := json.Marshal(body)
	if err != nil {
		return err
	}
	res, err := c.send(ctx, c.HTTP, http.MethodPost, c.url(path, nil), raw)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	if err := json.NewDecoder(res.Body).Decode(out); err != nil {
		return fmt.Errorf("decode %s: %w", path, err)
	}
	return nil
}

// ---------------------------------------------------------------------------
// Endpoints

type ServerInfo struct {
	BaseURL        string `json:"baseUrl"`
	Version        string `json:"version"`
	DeploymentType string `json:"deploymentType"`
	ServerTitle    string `json:"serverTitle"`
}

func (c *Client) ServerInfo(ctx context.Context) (ServerInfo, error) {
	var out ServerInfo
	err := c.get(ctx, c.api()+"/serverInfo", nil, &out)
	return out, err
}

func (c *Client) Myself(ctx context.Context) (User, error) {
	var out User
	err := c.get(ctx, c.api()+"/myself", nil, &out)
	return out, err
}

// User fetches one person by account id (Cloud) or key (Data Center).
func (c *Client) User(ctx context.Context, id string) (User, error) {
	var out User
	q := url.Values{}
	if c.Flavor == Cloud {
		q.Set("accountId", id)
	} else {
		q.Set("key", id)
	}
	err := c.get(ctx, c.api()+"/user", q, &out)
	if err != nil && c.Flavor != Cloud && isStatus(err, http.StatusNotFound) {
		q = url.Values{"username": {id}}
		err = c.get(ctx, c.api()+"/user", q, &out)
	}
	return out, err
}

// Projects lists every project the account can browse.
func (c *Client) Projects(ctx context.Context) ([]Project, error) {
	if c.Flavor != Cloud {
		var out []Project
		err := c.get(ctx, "/rest/api/2/project", nil, &out)
		return out, err
	}
	var all []Project
	for start := 0; ; {
		var page struct {
			Values []Project `json:"values"`
			IsLast bool      `json:"isLast"`
			Total  int       `json:"total"`
		}
		q := url.Values{"startAt": {strconv.Itoa(start)}, "maxResults": {"50"}, "orderBy": {"key"}}
		if err := c.get(ctx, "/rest/api/3/project/search", q, &page); err != nil {
			return nil, err
		}
		all = append(all, page.Values...)
		start += len(page.Values)
		if page.IsLast || len(page.Values) == 0 || (page.Total > 0 && start >= page.Total) {
			return all, nil
		}
	}
}

// Project returns a project with its issue types.
func (c *Client) Project(ctx context.Context, key string) (Project, error) {
	var out Project
	err := c.get(ctx, c.api()+"/project/"+url.PathEscape(key), nil, &out)
	return out, err
}

// ProjectStatuses lists the statuses of each issue type in a project's workflows.
func (c *Client) ProjectStatuses(ctx context.Context, key string) ([]TypeStatuses, error) {
	var out []TypeStatuses
	err := c.get(ctx, c.api()+"/project/"+url.PathEscape(key)+"/statuses", nil, &out)
	return out, err
}

func (c *Client) Statuses(ctx context.Context) ([]Status, error) {
	var out []Status
	err := c.get(ctx, c.api()+"/status", nil, &out)
	return out, err
}

func (c *Client) Priorities(ctx context.Context) ([]Priority, error) {
	var out []Priority
	err := c.get(ctx, c.api()+"/priority", nil, &out)
	return out, err
}

func (c *Client) Fields(ctx context.Context) ([]Field, error) {
	var out []Field
	err := c.get(ctx, c.api()+"/field", nil, &out)
	return out, err
}

func (c *Client) Versions(ctx context.Context, key string) ([]Version, error) {
	var out []Version
	err := c.get(ctx, c.api()+"/project/"+url.PathEscape(key)+"/versions", nil, &out)
	return out, err
}

// Boards lists the agile boards of a project (Jira Software). No agile API: none.
func (c *Client) Boards(ctx context.Context, projectKey string) ([]Board, error) {
	var all []Board
	for start := 0; ; {
		var page struct {
			Values []Board `json:"values"`
			IsLast bool    `json:"isLast"`
		}
		q := url.Values{"projectKeyOrId": {projectKey}, "startAt": {strconv.Itoa(start)}, "maxResults": {"50"}}
		if err := c.get(ctx, "/rest/agile/1.0/board", q, &page); err != nil {
			if isStatus(err, http.StatusNotFound, http.StatusForbidden, http.StatusBadRequest) {
				return all, nil
			}
			return nil, err
		}
		all = append(all, page.Values...)
		start += len(page.Values)
		if page.IsLast || len(page.Values) == 0 {
			return all, nil
		}
	}
}

// Sprints lists a board's sprints. Kanban boards have none (the API answers 400).
func (c *Client) Sprints(ctx context.Context, boardID int64) ([]Sprint, error) {
	var all []Sprint
	for start := 0; ; {
		var page struct {
			Values []Sprint `json:"values"`
			IsLast bool     `json:"isLast"`
		}
		q := url.Values{"startAt": {strconv.Itoa(start)}, "maxResults": {"50"}}
		if err := c.get(ctx, fmt.Sprintf("/rest/agile/1.0/board/%d/sprint", boardID), q, &page); err != nil {
			if isStatus(err, http.StatusNotFound, http.StatusForbidden, http.StatusBadRequest) {
				return all, nil
			}
			return nil, err
		}
		all = append(all, page.Values...)
		start += len(page.Values)
		if page.IsLast || len(page.Values) == 0 {
			return all, nil
		}
	}
}

// SearchPage is one page of search results and where the next one starts.
type SearchPage struct {
	Issues []Issue
	// Next is the Cloud nextPageToken or the Data Center startAt of the next page.
	Next string
	Last bool
}

// Search fetches one page of issues for a JQL query. Cloud uses the enhanced search
// (/search/jql, token pagination); Data Center the classic /search (startAt).
func (c *Client) Search(ctx context.Context, jql string, fields []string, expand string, next string, pageSize int) (SearchPage, error) {
	q := url.Values{"jql": {jql}, "maxResults": {strconv.Itoa(pageSize)}, "fields": {strings.Join(fields, ",")}}
	if expand != "" {
		q.Set("expand", expand)
	}
	if c.Flavor == Cloud {
		if next != "" {
			q.Set("nextPageToken", next)
		}
		var page struct {
			Issues        []Issue `json:"issues"`
			NextPageToken string  `json:"nextPageToken"`
			IsLast        *bool   `json:"isLast"`
		}
		if err := c.get(ctx, "/rest/api/3/search/jql", q, &page); err != nil {
			return SearchPage{}, err
		}
		last := page.NextPageToken == "" || len(page.Issues) == 0
		if page.IsLast != nil && *page.IsLast {
			last = true
		}
		return SearchPage{Issues: page.Issues, Next: page.NextPageToken, Last: last}, nil
	}
	start, _ := strconv.Atoi(next)
	q.Set("startAt", strconv.Itoa(start))
	var page struct {
		StartAt int     `json:"startAt"`
		Total   int     `json:"total"`
		Issues  []Issue `json:"issues"`
	}
	if err := c.get(ctx, "/rest/api/2/search", q, &page); err != nil {
		return SearchPage{}, err
	}
	end := page.StartAt + len(page.Issues)
	return SearchPage{Issues: page.Issues, Next: strconv.Itoa(end), Last: len(page.Issues) == 0 || end >= page.Total}, nil
}

// Count is how many issues a JQL query matches (approximate on Cloud), for progress totals.
func (c *Client) Count(ctx context.Context, jql string) (int, error) {
	if c.Flavor == Cloud {
		var out struct {
			Count int `json:"count"`
		}
		err := c.post(ctx, "/rest/api/3/search/approximate-count", map[string]string{"jql": jql}, &out)
		return out.Count, err
	}
	var out struct {
		Total int `json:"total"`
	}
	err := c.get(ctx, "/rest/api/2/search", url.Values{"jql": {jql}, "maxResults": {"0"}}, &out)
	return out.Total, err
}

// Sprint fetches one sprint (for sprints known only from an issue's changelog).
func (c *Client) Sprint(ctx context.Context, id int64) (Sprint, error) {
	var out Sprint
	err := c.get(ctx, fmt.Sprintf("/rest/agile/1.0/sprint/%d", id), nil, &out)
	return out, err
}

// Changelog returns an issue's complete change history, oldest first.
func (c *Client) Changelog(ctx context.Context, issueID string) ([]History, error) {
	if c.Flavor != Cloud {
		var issue Issue
		q := url.Values{"expand": {"changelog"}, "fields": {"none"}}
		if err := c.get(ctx, "/rest/api/2/issue/"+url.PathEscape(issueID), q, &issue); err != nil {
			return nil, err
		}
		if issue.Changelog == nil {
			return nil, nil
		}
		return issue.Changelog.Histories, nil
	}
	var all []History
	for start := 0; ; {
		var page struct {
			Values []History `json:"values"`
			IsLast bool      `json:"isLast"`
			Total  int       `json:"total"`
		}
		q := url.Values{"startAt": {strconv.Itoa(start)}, "maxResults": {"100"}}
		if err := c.get(ctx, "/rest/api/3/issue/"+url.PathEscape(issueID)+"/changelog", q, &page); err != nil {
			return nil, err
		}
		all = append(all, page.Values...)
		start += len(page.Values)
		if page.IsLast || len(page.Values) == 0 || (page.Total > 0 && start >= page.Total) {
			return all, nil
		}
	}
}

// Comments returns every comment of an issue, oldest first.
func (c *Client) Comments(ctx context.Context, issueID string) ([]Comment, error) {
	var all []Comment
	for start := 0; ; {
		var page CommentPage
		q := url.Values{"startAt": {strconv.Itoa(start)}, "maxResults": {"100"}, "orderBy": {"created"}}
		if err := c.get(ctx, c.api()+"/issue/"+url.PathEscape(issueID)+"/comment", q, &page); err != nil {
			return nil, err
		}
		all = append(all, page.Comments...)
		start += len(page.Comments)
		if len(page.Comments) == 0 || start >= page.Total {
			return all, nil
		}
	}
}

// OpenAttachment streams an attachment's content. The URL must be on the Jira site.
func (c *Client) OpenAttachment(ctx context.Context, a Attachment) (io.ReadCloser, error) {
	target := a.Content
	if target == "" || !strings.HasPrefix(target, c.BaseURL+"/") {
		if c.Flavor == Cloud {
			target = c.BaseURL + "/rest/api/3/attachment/content/" + url.PathEscape(a.ID)
		} else {
			target = c.BaseURL + "/secure/attachment/" + url.PathEscape(a.ID) + "/" + url.PathEscape(a.Filename)
		}
	}
	hc := c.Download
	if hc == nil {
		hc = c.HTTP
	}
	res, err := c.send(ctx, hc, http.MethodGet, target, nil)
	if err != nil {
		return nil, err
	}
	return res.Body, nil
}

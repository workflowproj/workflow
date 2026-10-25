package jiraimport

import (
	"context"
	"encoding/json"
	"net/url"
	"regexp"
	"strings"

	"github.com/google/uuid"

	"github.com/geoffo-dev/workflow-management/internal/db"
	"github.com/geoffo-dev/workflow-management/internal/db/dbq"
)

/*
 * People. A Jira person becomes, in this order:
 *   - what the admin chose in the mapping (an existing account, a placeholder, or nobody);
 *   - the account an earlier run of the same site mapped them to;
 *   - the existing account with their email;
 *   - a placeholder: an inactive account with their name and email that owns their history,
 *     comments and assignments. Nobody can sign in as a placeholder; signing in with that email
 *     through SSO (just-in-time provisioning), Google directory sync or SCIM activates it, which
 *     is how the person claims it. Without a visible email the placeholder gets a reserved
 *     ".invalid" address that no identity provider can verify, so nobody can claim it unless an
 *     admin maps the person.
 */

// person resolves a Jira user to an account here (nil = nobody).
func (r *run) person(ctx context.Context, u *User) (*uuid.UUID, error) {
	if u == nil || u.ID() == "" {
		return nil, nil
	}
	if u.Name != "" && u.Key != "" && u.Name != u.Key {
		r.aliases[u.Name] = u.Key
	}
	return r.resolvePerson(ctx, u.ID(), u)
}

// personByID resolves a Jira person known only by id (changelog values). Data Center
// changelogs may name people by username rather than key; those resolve to the key.
func (r *run) personByID(ctx context.Context, id, displayName string) (*uuid.UUID, error) {
	if id == "" {
		return nil, nil
	}
	if key, ok := r.aliases[id]; ok {
		id = key
	}
	if v, ok := r.people[id]; ok {
		return v, nil
	}
	if r.client.Flavor != Cloud {
		if full, err := r.client.User(ctx, id); err == nil && full.ID() != "" && full.ID() != id {
			r.aliases[id] = full.ID()
			return r.resolvePerson(ctx, full.ID(), &full)
		}
	}
	return r.resolvePerson(ctx, id, &User{AccountID: id, DisplayName: displayName})
}

func (r *run) resolvePerson(ctx context.Context, id string, u *User) (*uuid.UUID, error) {
	if v, ok := r.people[id]; ok {
		return v, nil
	}
	res, err := r.lookupPerson(ctx, id, u)
	if err != nil {
		return nil, err
	}
	r.people[id] = res
	return res, nil
}

func (r *run) lookupPerson(ctx context.Context, id string, u *User) (*uuid.UUID, error) {
	q := r.s.Q
	pm := r.cfg.People[id]
	switch pm.Action {
	case PersonSkip:
		return nil, nil
	case PersonUser:
		if pm.UserID != nil {
			if usr, err := q.GetUser(ctx, *pm.UserID); err == nil {
				r.remember(usr)
				return &usr.ID, r.mapPerson(ctx, id, usr.ID)
			}
		}
	}
	if pm.Action != PersonPlaceholder || pm.Email == "" {
		if m, err := q.GetJiraMap(ctx, dbq.GetJiraMapParams{Site: r.site, Kind: "user", JiraID: id}); err == nil {
			if usr, err := q.GetUser(ctx, m.LocalID); err == nil {
				r.remember(usr)
				return &usr.ID, nil
			}
		} else if !db.IsNoRows(err) {
			return nil, err
		}
	}
	email := strings.ToLower(strings.TrimSpace(u.EmailAddress))
	name := cmpOr(u.DisplayName, u.Name)
	if p := r.scanPerson(id); p != nil {
		email = cmpOr(email, p.Email)
		name = cmpOr(name, p.DisplayName)
	}
	if email == "" || name == "" {
		// Changelog values carry only an id and a name: ask Jira for the rest.
		if full, err := r.client.User(ctx, id); err == nil {
			email = cmpOr(email, strings.ToLower(full.EmailAddress))
			name = cmpOr(name, full.DisplayName)
		}
	}
	if pm.Action == PersonPlaceholder && pm.Email != "" {
		email = pm.Email
	}
	if email != "" {
		if usr, err := q.GetUserByEmail(ctx, email); err == nil {
			r.remember(usr)
			return &usr.ID, r.mapPerson(ctx, id, usr.ID)
		} else if !db.IsNoRows(err) {
			return nil, err
		}
	}
	return r.placeholder(ctx, id, cmpOr(name, id), email)
}

func (r *run) scanPerson(id string) *ScanPerson {
	if r.scan == nil {
		return nil
	}
	for i := range r.scan.People {
		if r.scan.People[i].ID == id {
			return &r.scan.People[i]
		}
	}
	return nil
}

var emailUnsafe = regexp.MustCompile(`[^a-z0-9._-]+`)

// unclaimableEmail is a reserved address (RFC 2606 .invalid) for people without a visible email.
func (r *run) unclaimableEmail(id string) string {
	host := "jira"
	if u, err := url.Parse(r.imp.BaseUrl); err == nil && u.Hostname() != "" {
		host = u.Hostname()
	}
	local := strings.Trim(emailUnsafe.ReplaceAllString(strings.ToLower(id), "-"), "-.")
	if len(local) > 60 {
		local = local[:60]
	}
	return "jira-" + cmpOr(local, "user") + "@" + emailUnsafe.ReplaceAllString(host, "-") + ".invalid"
}

func (r *run) placeholder(ctx context.Context, id, name, email string) (*uuid.UUID, error) {
	if email == "" {
		email = r.unclaimableEmail(id)
	}
	var out uuid.UUID
	err := db.Tx(ctx, r.s.Pool, func(_ pgxTx, q *dbq.Queries) error {
		usr, err := q.GetUserByEmail(ctx, email)
		if db.IsNoRows(err) {
			usr, err = q.CreateUser(ctx, dbq.CreateUserParams{
				Kind: "human", Email: email, Username: email, DisplayName: name, OrgRole: "member", Source: "admin", Active: false,
			})
			if err != nil {
				return err
			}
			if err := q.CreateUserPlaceholder(ctx, dbq.CreateUserPlaceholderParams{UserID: usr.ID, Source: "jira", ExternalID: id}); err != nil {
				return err
			}
			r.prog.count("placeholders", 1)
		} else if err != nil {
			return err
		}
		out = usr.ID
		r.names[usr.ID] = usr.DisplayName
		r.active[usr.ID] = usr.Active
		return upsertMap(ctx, q, r.site, "user", id, usr.ID, nil, &r.imp.ID, nil)
	})
	if err != nil {
		return nil, err
	}
	return &out, nil
}

func (r *run) mapPerson(ctx context.Context, id string, userID uuid.UUID) error {
	r.prog.count("peopleMatched", 1)
	return upsertMap(ctx, r.s.Q, r.site, "user", id, userID, nil, &r.imp.ID, nil)
}

func (r *run) remember(u dbq.User) {
	r.names[u.ID] = u.DisplayName
	r.active[u.ID] = u.Active
}

// userName is the display name here of a resolved person (for history values).
func (r *run) userName(ctx context.Context, id *uuid.UUID) *string {
	if id == nil {
		return nil
	}
	if n, ok := r.names[*id]; ok {
		return &n
	}
	u, err := r.s.Q.GetUser(ctx, *id)
	if err != nil {
		return nil
	}
	r.remember(u)
	return &u.DisplayName
}

func upsertMap(ctx context.Context, q *dbq.Queries, site, kind, jiraID string, localID uuid.UUID, projectID, importID *uuid.UUID, data any) error {
	raw := []byte("{}")
	if data != nil {
		var err error
		if raw, err = json.Marshal(data); err != nil {
			return err
		}
	}
	return q.UpsertJiraMap(ctx, dbq.UpsertJiraMapParams{Site: site, Kind: kind, JiraID: jiraID, LocalID: localID, ProjectID: projectID, ImportID: importID, Data: raw})
}

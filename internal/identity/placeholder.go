package identity

import (
	"context"
	"strings"

	"github.com/geoffo-dev/workflow-management/internal/authz"
	"github.com/geoffo-dev/workflow-management/internal/db"
	"github.com/geoffo-dev/workflow-management/internal/db/dbq"
	"github.com/geoffo-dev/workflow-management/internal/tracker"
)

// claimPlaceholder hands a placeholder account (a person known only from an import, see
// user_placeholders) to the person signing in with its email: the account is activated and
// becomes a normal account of the given source, keeping their imported history, comments and
// assignments. Placeholders have no credentials, so this is the only way into them; it follows
// the just-in-time provisioning rule, since claiming is creating an account on first sign-in.
// Accounts that are not placeholders are returned unchanged.
func (p *Provisioner) claimPlaceholder(ctx context.Context, q *dbq.Queries, u dbq.User, source string, needJIT bool) (dbq.User, error) {
	if u.Active || u.DeletedAt != nil || u.Kind != "human" || strings.HasSuffix(u.Email, ".invalid") {
		return u, nil
	}
	ok, err := q.IsUserPlaceholder(ctx, u.ID)
	if err != nil || !ok {
		return u, err
	}
	if needJIT && !p.JITEnabled(ctx) {
		return u, &LoginError{"not_provisioned"}
	}
	claimed, err := q.ClaimUserPlaceholder(ctx, dbq.ClaimUserPlaceholderParams{ID: u.ID, Source: source})
	if db.IsNoRows(err) {
		return u, nil // claimed concurrently, or no longer a placeholder
	}
	if err != nil {
		return u, err
	}
	tracker.Audit(ctx, q, authz.SystemActor(), "user.claim_placeholder", "user", u.ID.String(), nil, map[string]any{"email": u.Email, "source": source})
	return claimed, nil
}

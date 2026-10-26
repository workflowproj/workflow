package search

import (
	"fmt"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/workflowproj/workflow/internal/timetrack"
)

// CustomField describes a custom field the compiler may reference by key or name.
type CustomField struct {
	Key  string
	Name string
	Type string // text, number, select, multiselect, user, date, version, checkbox, url
}

// Env supplies context for functions such as currentUser() and relative dates.
type Env struct {
	UserID       *uuid.UUID
	Now          time.Time
	CustomFields []CustomField
	// Units turn durations such as 1d into seconds (the default 8-hour day when unset).
	Units timetrack.Units
	// SLACalendars lists the calendars of SLA clocks for remaining() (called only when a query
	// uses it; nil means every clock runs around the clock).
	SLACalendars func() ([]SLACalendar, error)
}

// Compiled is a WHERE expression and ORDER BY list over the issues table aliased as `i`.
type Compiled struct {
	Where   string
	OrderBy string
	Args    []any
}

type builder struct {
	env  Env
	args []any
	base int
}

func (b *builder) arg(v any) string {
	b.args = append(b.args, v)
	return "$" + strconv.Itoa(b.base+len(b.args))
}

// Compile turns a parsed query into SQL. argOffset is the number of positional parameters the
// caller already uses; defaultOrder applies when the query has no ORDER BY.
func Compile(q *Query, env Env, argOffset int, defaultOrder []Order) (*Compiled, error) {
	if env.Now.IsZero() {
		env.Now = time.Now().UTC()
	}
	if env.Units.HoursPerDay <= 0 || env.Units.DaysPerWeek <= 0 {
		env.Units = timetrack.Default
	}
	b := &builder{env: env, base: argOffset}
	where := "TRUE"
	if q.Where != nil {
		w, err := b.node(q.Where)
		if err != nil {
			return nil, err
		}
		where = w
	}
	orders := q.OrderBy
	if len(orders) == 0 {
		orders = defaultOrder
	}
	var parts []string
	for _, o := range orders {
		expr, err := orderExpr(o)
		if err != nil {
			return nil, err
		}
		dir := " ASC NULLS LAST"
		if o.Desc {
			dir = " DESC NULLS LAST"
		}
		parts = append(parts, expr+dir)
	}
	parts = append(parts, "i.id")
	return &Compiled{Where: where, OrderBy: strings.Join(parts, ", "), Args: b.args}, nil
}

// CompileString parses and compiles in one step.
func CompileString(src string, env Env, argOffset int, defaultOrder []Order) (*Compiled, error) {
	q, err := Parse(src)
	if err != nil {
		return nil, err
	}
	return Compile(q, env, argOffset, defaultOrder)
}

func (b *builder) node(n Node) (string, error) {
	switch n := n.(type) {
	case And:
		l, err := b.node(n.Left)
		if err != nil {
			return "", err
		}
		r, err := b.node(n.Right)
		if err != nil {
			return "", err
		}
		return "(" + l + " AND " + r + ")", nil
	case Or:
		l, err := b.node(n.Left)
		if err != nil {
			return "", err
		}
		r, err := b.node(n.Right)
		if err != nil {
			return "", err
		}
		return "(" + l + " OR " + r + ")", nil
	case Not:
		x, err := b.node(n.X)
		if err != nil {
			return "", err
		}
		return "(NOT coalesce(" + x + ", false))", nil
	case Clause:
		return b.clause(n)
	}
	return "", fmt.Errorf("unknown node %T", n)
}

func normalize(field string) string {
	return strings.NewReplacer(" ", "", "_", "", "-", "").Replace(strings.ToLower(field))
}

var cfPattern = regexp.MustCompile(`^cf\[(.+)\]$`)

func (b *builder) clause(c Clause) (string, error) {
	switch normalize(c.Field) {
	case "project":
		return b.setField(c, func(v Value) (string, error) {
			s, err := b.str(v)
			if err != nil {
				return "", err
			}
			p := b.arg(s)
			return fmt.Sprintf("i.project_id IN (SELECT id FROM projects WHERE key = upper(%[1]s::text) OR id::text = %[1]s::text UNION SELECT project_id FROM project_key_aliases WHERE key = upper(%[1]s::text))", p), nil
		}, "")
	case "key", "issuekey", "issue", "id":
		return b.keyField(c)
	case "status":
		return b.setField(c, func(v Value) (string, error) {
			s, err := b.str(v)
			if err != nil {
				return "", err
			}
			p := b.arg(s)
			return fmt.Sprintf("i.status_id IN (SELECT id FROM statuses WHERE lower(name) = lower(%[1]s::text) OR id::text = %[1]s::text)", p), nil
		}, "")
	case "statuscategory", "category":
		return b.setField(c, func(v Value) (string, error) {
			s, err := b.str(v)
			if err != nil {
				return "", err
			}
			cat, ok := map[string]string{"todo": "todo", "inprogress": "in_progress", "done": "done", "new": "todo", "indeterminate": "in_progress", "complete": "done"}[normalize(s)]
			if !ok {
				return "", errAt(v.Pos, "unknown status category %q (use \"To Do\", \"In Progress\" or \"Done\")", s)
			}
			return fmt.Sprintf("i.status_id IN (SELECT id FROM statuses WHERE category = %s::text)", b.arg(cat)), nil
		}, "")
	case "type", "issuetype":
		return b.setField(c, func(v Value) (string, error) {
			s, err := b.str(v)
			if err != nil {
				return "", err
			}
			p := b.arg(s)
			return fmt.Sprintf("i.type_id IN (SELECT id FROM issue_types WHERE lower(key) = lower(%[1]s::text) OR lower(name) = lower(%[1]s::text))", p), nil
		}, "")
	case "kind", "typekind", "issuetypekind":
		// Work, or the risks and decisions kept out of planning.
		return b.setField(c, func(v Value) (string, error) {
			s, err := b.str(v)
			if err != nil {
				return "", err
			}
			k := strings.ToLower(s)
			if !slices.Contains([]string{"work", "risk", "decision"}, k) {
				return "", errAt(v.Pos, "unknown kind %q (use work, risk or decision)", s)
			}
			return fmt.Sprintf("i.type_id IN (SELECT id FROM issue_types WHERE kind = %s::text)", b.arg(k)), nil
		}, "")
	case "level", "hierarchylevel":
		return b.numberField(c, "(SELECT level FROM issue_types t WHERE t.id = i.type_id)")
	case "assignee":
		return b.userField(c, "i.assignee_id")
	case "reporter":
		return b.userField(c, "i.reporter_id")
	case "watcher", "watchers":
		return b.setField(c, func(v Value) (string, error) {
			u, err := b.userSet(v)
			if err != nil {
				return "", err
			}
			return "EXISTS (SELECT 1 FROM watchers w WHERE w.issue_id = i.id AND w.user_id IN (" + u + "))", nil
		}, "NOT EXISTS (SELECT 1 FROM watchers w WHERE w.issue_id = i.id)")
	case "priority":
		return b.priorityField(c)
	case "resolution":
		return b.setField(c, func(v Value) (string, error) {
			s, err := b.str(v)
			if err != nil {
				return "", err
			}
			if strings.EqualFold(s, "unresolved") {
				return "i.resolution IS NULL", nil
			}
			return fmt.Sprintf("lower(i.resolution) = lower(%s::text)", b.arg(s)), nil
		}, "i.resolution IS NULL")
	case "labels", "label":
		return b.setField(c, func(v Value) (string, error) {
			s, err := b.str(v)
			if err != nil {
				return "", err
			}
			return fmt.Sprintf("%s::text = ANY(i.labels)", b.arg(s)), nil
		}, "cardinality(i.labels) = 0")
	case "sprint":
		return b.setField(c, func(v Value) (string, error) {
			if v.Kind == VFunc {
				state, ok := map[string]string{"opensprints": "active", "futuresprints": "future", "closedsprints": "closed"}[strings.ToLower(v.Text)]
				if !ok {
					return "", errAt(v.Pos, "unknown function %s() for sprint", v.Text)
				}
				return fmt.Sprintf("i.sprint_id IN (SELECT id FROM sprints WHERE state = %s::text)", b.arg(state)), nil
			}
			s, err := b.str(v)
			if err != nil {
				return "", err
			}
			p := b.arg(s)
			return fmt.Sprintf("i.sprint_id IN (SELECT id FROM sprints WHERE lower(name) = lower(%[1]s::text) OR id::text = %[1]s::text)", p), nil
		}, "i.sprint_id IS NULL")
	case "fixversion", "release", "version", "fixversions":
		return b.setField(c, func(v Value) (string, error) {
			cond := ""
			if v.Kind == VFunc {
				state, ok := map[string]string{"releasedversions": "released", "unreleasedversions": "unreleased"}[strings.ToLower(v.Text)]
				if !ok {
					return "", errAt(v.Pos, "unknown function %s() for fixVersion", v.Text)
				}
				cond = fmt.Sprintf("r.state = %s::text", b.arg(state))
			} else {
				s, err := b.str(v)
				if err != nil {
					return "", err
				}
				p := b.arg(s)
				cond = fmt.Sprintf("(lower(r.name) = lower(%[1]s::text) OR r.id::text = %[1]s::text)", p)
			}
			return "EXISTS (SELECT 1 FROM issue_releases ir JOIN releases r ON r.id = ir.release_id WHERE ir.issue_id = i.id AND " + cond + ")", nil
		}, "NOT EXISTS (SELECT 1 FROM issue_releases ir WHERE ir.issue_id = i.id)")
	case "deployedto":
		// Environments a successful deployment containing the issue reached.
		const deployed = "SELECT 1 FROM deployment_issues di JOIN deployments d ON d.id = di.deployment_id WHERE di.issue_id = i.id AND d.status = 'success'"
		return b.setField(c, func(v Value) (string, error) {
			s, err := b.str(v)
			if err != nil {
				return "", err
			}
			return fmt.Sprintf("EXISTS (%s AND lower(d.environment) = lower(%s::text))", deployed, b.arg(s)), nil
		}, "NOT EXISTS ("+deployed+")")
	case "parent":
		return b.setField(c, func(v Value) (string, error) {
			sub, err := b.issueByKey(v)
			if err != nil {
				return "", err
			}
			return "i.parent_id IN (" + sub + ")", nil
		}, "i.parent_id IS NULL")
	case "epic", "epiclink":
		// The epic ancestor: the parent when it is epic-level, else the grandparent (sub-tasks).
		return b.setField(c, func(v Value) (string, error) {
			sub, err := b.issueByKey(v)
			if err != nil {
				return "", err
			}
			return inEpics("SELECT e.id FROM issues e JOIN issue_types et ON et.id = e.type_id WHERE et.level >= 1 AND e.id IN (" + sub + ")"), nil
		}, "NOT coalesce("+inEpics(allEpics)+", false)")
	case "issuelinktype", "linktype":
		return b.setField(c, func(v Value) (string, error) {
			s, err := b.str(v)
			if err != nil {
				return "", err
			}
			l, ok := linkDescriptions[normalize(s)]
			if !ok {
				return "", errAt(v.Pos, "unknown link type %q (use %s)", s, linkTypeHelp)
			}
			return l.exists(b.arg(l.typ), "i.id"), nil
		}, "NOT EXISTS (SELECT 1 FROM issue_links l WHERE l.source_id = i.id OR l.target_id = i.id)")
	case "summary", "title":
		return b.textField(c, "i.summary")
	case "description":
		return b.textField(c, "i.description")
	case "text":
		return b.fullText(c)
	case "comment", "comments":
		return b.commentField(c)
	case "created", "createddate":
		return b.dateField(c, "i.created_at", false)
	case "updated", "updateddate":
		return b.dateField(c, "i.updated_at", false)
	case "resolved", "resolutiondate":
		return b.dateField(c, "i.resolved_at", false)
	case "due", "duedate":
		return b.dateField(c, "i.due_date", true)
	case "start", "startdate":
		return b.dateField(c, "i.start_date", true)
	case "storypoints", "points", "estimate":
		return b.numberField(c, "i.story_points")
	case "originalestimate", "timeoriginalestimate":
		return b.durationField(c, "i.original_estimate_seconds", false)
	case "remainingestimate", "timeestimate":
		return b.durationField(c, "i.remaining_estimate_seconds", false)
	case "timespent":
		return b.durationField(c, timeSpentExpr, true)
	case "worklogauthor":
		return b.setField(c, func(v Value) (string, error) {
			u, err := b.userSet(v)
			if err != nil {
				return "", err
			}
			return worklogExists + " AND w.author_id IN (" + u + "))", nil
		}, "NOT "+worklogExists+")")
	case "worklogdate":
		return b.worklogDate(c)
	case "timetofirstresponse", "firstresponse":
		return b.slaField(c, "first_response")
	case "timetoresolution":
		return b.slaField(c, "resolution")
	case "requesttype", "customerrequesttype":
		return b.requestTypeField(c)
	case "approval", "approvals":
		return b.approvalField(c)
	case "participant", "participants", "requestparticipants":
		return b.participantField(c)
	}
	if cf, ok := b.customField(c.Field); ok {
		return b.customClause(c, cf)
	}
	return "", errAt(c.Pos, "unknown field %q", c.Field)
}

func (b *builder) customField(name string) (CustomField, bool) {
	if m := cfPattern.FindStringSubmatch(strings.ToLower(name)); m != nil {
		name = m[1]
	}
	for _, f := range b.env.CustomFields {
		if strings.EqualFold(f.Key, name) || strings.EqualFold(f.Name, name) {
			return f, true
		}
	}
	return CustomField{}, false
}

// setField composes =, !=, in, not in, is [not] EMPTY from a per-value condition.
func (b *builder) setField(c Clause, eq func(Value) (string, error), empty string) (string, error) {
	one := func(v Value) (string, error) {
		if v.Kind == VEmpty {
			if empty == "" {
				return "", errAt(v.Pos, "%s cannot be EMPTY", c.Field)
			}
			return empty, nil
		}
		return eq(v)
	}
	anyOf := func() (string, error) {
		var parts []string
		for _, v := range c.Values {
			s, err := one(v)
			if err != nil {
				return "", err
			}
			parts = append(parts, s)
		}
		return "(" + strings.Join(parts, " OR ") + ")", nil
	}
	switch c.Op {
	case "=", "in", "is":
		return anyOf()
	case "!=", "not in", "is not":
		s, err := anyOf()
		if err != nil {
			return "", err
		}
		return "(NOT coalesce(" + s + ", false))", nil
	}
	return "", errAt(c.Pos, "operator %q is not supported for %s", c.Op, c.Field)
}

func (b *builder) str(v Value) (string, error) {
	switch v.Kind {
	case VString, VNumber:
		return v.Text, nil
	case VFunc:
		return "", errAt(v.Pos, "function %s() is not supported here", v.Text)
	}
	return "", errAt(v.Pos, "expected a value")
}

// userSet returns a subquery (or parameter) yielding user ids for a value.
func (b *builder) userSet(v Value) (string, error) {
	if v.Kind == VFunc {
		switch strings.ToLower(v.Text) {
		case "currentuser":
			if b.env.UserID == nil {
				return "SELECT NULL::uuid WHERE false", nil
			}
			return b.arg(*b.env.UserID) + "::uuid", nil
		case "membersof":
			if len(v.Args) != 1 {
				return "", errAt(v.Pos, "membersOf() takes a group name")
			}
			g, err := b.str(v.Args[0])
			if err != nil {
				return "", err
			}
			return fmt.Sprintf("SELECT gm.user_id FROM group_members gm JOIN groups g ON g.id = gm.group_id WHERE lower(g.display_name) = lower(%[1]s::text) OR lower(coalesce(g.email, '')) = lower(%[1]s::text)", b.arg(g)), nil
		}
		return "", errAt(v.Pos, "unknown function %s()", v.Text)
	}
	s, err := b.str(v)
	if err != nil {
		return "", err
	}
	if id, err := uuid.Parse(s); err == nil {
		return b.arg(id) + "::uuid", nil
	}
	p := b.arg(s)
	return fmt.Sprintf("SELECT id FROM users WHERE email = lower(%[1]s::text) OR lower(username) = lower(%[1]s::text) OR lower(display_name) = lower(%[1]s::text)", p), nil
}

func (b *builder) userField(c Clause, col string) (string, error) {
	return b.setField(c, func(v Value) (string, error) {
		u, err := b.userSet(v)
		if err != nil {
			return "", err
		}
		return col + " IN (" + u + ")", nil
	}, col+" IS NULL")
}

var keyPattern = regexp.MustCompile(`^([A-Za-z][A-Za-z0-9]{1,9})-(\d+)$`)

func (b *builder) parseKey(v Value) (string, int, error) {
	s, err := b.str(v)
	if err != nil {
		return "", 0, err
	}
	m := keyPattern.FindStringSubmatch(strings.TrimSpace(s))
	if m == nil {
		return "", 0, errAt(v.Pos, "%q is not an issue key", s)
	}
	n, _ := strconv.Atoi(m[2])
	return strings.ToUpper(m[1]), n, nil
}

func projectByKey(param string) string {
	return fmt.Sprintf("(SELECT id FROM projects WHERE key = %[1]s::text UNION SELECT project_id FROM project_key_aliases WHERE key = %[1]s::text)", param)
}

func (b *builder) issueByKey(v Value) (string, error) {
	key, n, err := b.parseKey(v)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("SELECT x.id FROM issues x WHERE x.number = %s::int AND x.project_id IN %s", b.arg(n), projectByKey(b.arg(key))), nil
}

func (b *builder) keyField(c Clause) (string, error) {
	switch c.Op {
	case ">", ">=", "<", "<=":
		key, n, err := b.parseKey(c.Values[0])
		if err != nil {
			return "", err
		}
		return fmt.Sprintf("(i.project_id IN %s AND i.number %s %s::int)", projectByKey(b.arg(key)), c.Op, b.arg(n)), nil
	}
	return b.setField(c, func(v Value) (string, error) {
		if v.Kind == VFunc {
			return b.issueFunction(v)
		}
		sub, err := b.issueByKey(v)
		if err != nil {
			return "", err
		}
		return "i.id IN (" + sub + ")", nil
	}, "")
}

// inEpics is "the issue's epic is one of these": its parent is one of the epics, or its parent
// is a standard issue whose parent is (sub-tasks). Both sides are sets of parent ids, so the
// database looks them up once rather than walking every issue's parents.
func inEpics(epics string) string {
	return "(i.parent_id IN (" + epics + ") OR i.parent_id IN (SELECT c.id FROM issues c JOIN issue_types ct ON ct.id = c.type_id WHERE ct.level < 1 AND c.parent_id IN (" + epics + ")))"
}

const allEpics = "SELECT e.id FROM issues e JOIN issue_types et ON et.id = e.type_id WHERE et.level >= 1"

// linkDescription is one side of a link type, as people word it: "blocks" is the source's side
// of a blocks link, "is blocked by" the target's.
type linkDescription struct {
	typ  string
	side string // source, target or either
}

var linkDescriptions = map[string]linkDescription{
	"blocks": {"blocks", "source"}, "isblockedby": {"blocks", "target"}, "blockedby": {"blocks", "target"},
	"relates": {"relates", "either"}, "relatesto": {"relates", "either"},
	"duplicates": {"duplicates", "source"}, "isduplicatedby": {"duplicates", "target"},
	"clones": {"clones", "source"}, "isclonedby": {"clones", "target"},
	// Work mitigates a risk, work implements a decision, a decision supersedes an earlier one.
	"mitigates": {"mitigates", "source"}, "ismitigatedby": {"mitigates", "target"}, "mitigatedby": {"mitigates", "target"},
	"implements": {"implements", "source"}, "isimplementedby": {"implements", "target"}, "implementedby": {"implements", "target"},
	"supersedes": {"supersedes", "source"}, "issupersededby": {"supersedes", "target"}, "supersededby": {"supersedes", "target"},
}

const linkTypeHelp = `blocks, "is blocked by", relates, duplicates, "is duplicated by", clones, "is cloned by", ` +
	`mitigates, "is mitigated by", implements, "is implemented by", supersedes or "is superseded by"`

// exists is "issue col has a link of this description".
func (l linkDescription) exists(typ, col string) string {
	switch l.side {
	case "source":
		return fmt.Sprintf("EXISTS (SELECT 1 FROM issue_links l WHERE l.source_id = %s AND l.link_type = %s::text)", col, typ)
	case "target":
		return fmt.Sprintf("EXISTS (SELECT 1 FROM issue_links l WHERE l.target_id = %s AND l.link_type = %s::text)", col, typ)
	}
	return fmt.Sprintf("EXISTS (SELECT 1 FROM issue_links l WHERE (l.source_id = %[1]s OR l.target_id = %[1]s) AND l.link_type = %[2]s::text)", col, typ)
}

// issueFunction compiles issue functions used with key/issue (issue in linkedIssues("PLAT-1")).
func (b *builder) issueFunction(v Value) (string, error) {
	const openIssue = "EXISTS (SELECT 1 FROM issues o JOIN statuses os ON os.id = o.status_id WHERE o.id = %s AND os.category <> 'done')"
	switch strings.ToLower(v.Text) {
	case "linkedissues":
		if len(v.Args) < 1 || len(v.Args) > 2 {
			return "", errAt(v.Pos, `linkedIssues() takes an issue key and optionally a link type, e.g. linkedIssues("PLAT-1", "is blocked by")`)
		}
		sub, err := b.issueByKey(v.Args[0])
		if err != nil {
			return "", err
		}
		if len(v.Args) == 1 {
			return "i.id IN (SELECT l.target_id FROM issue_links l WHERE l.source_id IN (" + sub + ") UNION SELECT l.source_id FROM issue_links l WHERE l.target_id IN (" + sub + "))", nil
		}
		s, err := b.str(v.Args[1])
		if err != nil {
			return "", err
		}
		l, ok := linkDescriptions[normalize(s)]
		if !ok {
			return "", errAt(v.Args[1].Pos, "unknown link type %q (use %s)", s, linkTypeHelp)
		}
		// "PLAT-1 blocks X": X is the target of PLAT-1's blocks links, and so on.
		t := b.arg(l.typ)
		switch l.side {
		case "source":
			return fmt.Sprintf("i.id IN (SELECT l.target_id FROM issue_links l WHERE l.link_type = %s::text AND l.source_id IN (%s))", t, sub), nil
		case "target":
			return fmt.Sprintf("i.id IN (SELECT l.source_id FROM issue_links l WHERE l.link_type = %s::text AND l.target_id IN (%s))", t, sub), nil
		}
		return fmt.Sprintf("i.id IN (SELECT l.target_id FROM issue_links l WHERE l.link_type = %[1]s::text AND l.source_id IN (%[2]s) UNION SELECT l.source_id FROM issue_links l WHERE l.link_type = %[1]s::text AND l.target_id IN (%[2]s))", t, sub), nil
	case "blockedissues":
		// Blocked by at least one issue that is not done.
		return "EXISTS (SELECT 1 FROM issue_links l WHERE l.target_id = i.id AND l.link_type = 'blocks' AND " + fmt.Sprintf(openIssue, "l.source_id") + ")", nil
	case "blockingissues":
		// Blocking at least one issue that is not done.
		return "EXISTS (SELECT 1 FROM issue_links l WHERE l.source_id = i.id AND l.link_type = 'blocks' AND " + fmt.Sprintf(openIssue, "l.target_id") + ")", nil
	}
	return "", errAt(v.Pos, "unknown function %s() for issue (use linkedIssues, blockedIssues or blockingIssues)", v.Text)
}

var priorityOrder = []string{"lowest", "low", "medium", "high", "highest"}

const priorityRank = "array_position(ARRAY['lowest','low','medium','high','highest']::text[], i.priority)"

func (b *builder) priorityField(c Clause) (string, error) {
	rank := func(v Value) (int, error) {
		s, err := b.str(v)
		if err != nil {
			return 0, err
		}
		idx := slices.Index(priorityOrder, strings.ToLower(s))
		if idx < 0 {
			return 0, errAt(v.Pos, "unknown priority %q", s)
		}
		return idx + 1, nil
	}
	switch c.Op {
	case ">", ">=", "<", "<=":
		r, err := rank(c.Values[0])
		if err != nil {
			return "", err
		}
		return fmt.Sprintf("%s %s %s::int", priorityRank, c.Op, b.arg(r)), nil
	}
	return b.setField(c, func(v Value) (string, error) {
		r, err := rank(v)
		if err != nil {
			return "", err
		}
		return fmt.Sprintf("i.priority = %s::text", b.arg(priorityOrder[r-1])), nil
	}, "")
}

func likePattern(s string) string {
	return "%" + strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(s) + "%"
}

func (b *builder) textField(c Clause, col string) (string, error) {
	v := c.Values[0]
	switch c.Op {
	case "~", "!~":
		s, err := b.str(v)
		if err != nil {
			return "", err
		}
		cond := fmt.Sprintf("%s ILIKE %s::text", col, b.arg(likePattern(s)))
		if c.Op == "!~" {
			return "(NOT " + cond + ")", nil
		}
		return cond, nil
	case "is", "is not":
		cond := "coalesce(" + col + ", '') = ''"
		if c.Op == "is not" {
			return "(NOT " + cond + ")", nil
		}
		return cond, nil
	case "=", "!=":
		s, err := b.str(v)
		if err != nil {
			return "", err
		}
		cond := fmt.Sprintf("lower(%s) = lower(%s::text)", col, b.arg(s))
		if c.Op == "!=" {
			return "(NOT " + cond + ")", nil
		}
		return cond, nil
	}
	return "", errAt(c.Pos, "operator %q is not supported for %s (use ~ to search text)", c.Op, c.Field)
}

func (b *builder) fullText(c Clause) (string, error) {
	if c.Op != "~" && c.Op != "!~" {
		return "", errAt(c.Pos, "text only supports ~ and !~")
	}
	s, err := b.str(c.Values[0])
	if err != nil {
		return "", err
	}
	// Exact terms, or English word forms ("backups" finds "backup"); each document has its own index.
	q := b.arg(s)
	cond := fmt.Sprintf("(issue_search_document(i.summary, i.description) @@ websearch_to_tsquery('simple', %s::text) OR issue_search_document_english(i.summary, i.description) @@ websearch_to_tsquery('english', %s::text) OR i.summary ILIKE %s::text)", q, q, b.arg(likePattern(s)))
	if c.Op == "!~" {
		return "(NOT " + cond + ")", nil
	}
	return cond, nil
}

func (b *builder) commentField(c Clause) (string, error) {
	if c.Op != "~" && c.Op != "!~" {
		return "", errAt(c.Pos, "comment only supports ~ and !~")
	}
	s, err := b.str(c.Values[0])
	if err != nil {
		return "", err
	}
	cond := fmt.Sprintf("EXISTS (SELECT 1 FROM comments cm WHERE cm.issue_id = i.id AND cm.body ILIKE %s::text)", b.arg(likePattern(s)))
	if c.Op == "!~" {
		return "(NOT " + cond + ")", nil
	}
	return cond, nil
}

func (b *builder) numberField(c Clause, col string) (string, error) {
	num := func(v Value) (float64, error) {
		if v.Kind != VNumber && v.Kind != VString {
			return 0, errAt(v.Pos, "expected a number")
		}
		f, err := strconv.ParseFloat(v.Text, 64)
		if err != nil {
			return 0, errAt(v.Pos, "%q is not a number", v.Text)
		}
		return f, nil
	}
	switch c.Op {
	case ">", ">=", "<", "<=":
		f, err := num(c.Values[0])
		if err != nil {
			return "", err
		}
		return fmt.Sprintf("%s %s %s::numeric", col, c.Op, b.arg(f)), nil
	}
	return b.setField(c, func(v Value) (string, error) {
		f, err := num(v)
		if err != nil {
			return "", err
		}
		return fmt.Sprintf("%s = %s::numeric", col, b.arg(f)), nil
	}, col+" IS NULL")
}

// timeSpentExpr is an issue's time spent: the sum of its worklogs (NULL without any).
const timeSpentExpr = "(SELECT sum(w.duration_seconds) FROM worklogs w WHERE w.issue_id = i.id)"

// worklogExists opens a condition on the issue's worklogs (alias w); callers close it.
const worklogExists = "EXISTS (SELECT 1 FROM worklogs w WHERE w.issue_id = i.id"

// durationField compares a duration column (seconds) with values such as 2h, "1d 4h" or a
// bare number of minutes. With zeroWhenEmpty an issue without a value compares as 0 (time
// spent); is EMPTY still means no value.
func (b *builder) durationField(c Clause, col string, zeroWhenEmpty bool) (string, error) {
	dur := func(v Value) (int64, error) {
		if v.Kind != VNumber && v.Kind != VString {
			return 0, errAt(v.Pos, "expected a duration such as 2h or \"1d 4h\"")
		}
		n, err := timetrack.Parse(v.Text, b.env.Units)
		if err != nil {
			return 0, errAt(v.Pos, "%q is not a duration (use w, d, h and m, e.g. 4h or \"1d 2h\"; a bare number is minutes)", v.Text)
		}
		return n, nil
	}
	cmp := col
	if zeroWhenEmpty {
		cmp = "coalesce(" + col + ", 0)"
	}
	switch c.Op {
	case ">", ">=", "<", "<=":
		n, err := dur(c.Values[0])
		if err != nil {
			return "", err
		}
		return fmt.Sprintf("%s %s %s::bigint", cmp, c.Op, b.arg(n)), nil
	}
	return b.setField(c, func(v Value) (string, error) {
		n, err := dur(v)
		if err != nil {
			return "", err
		}
		return fmt.Sprintf("%s = %s::bigint", cmp, b.arg(n)), nil
	}, col+" IS NULL")
}

// worklogDate matches issues with work logged on (or before, after...) a date; != and not in
// match issues with no work logged then, and is EMPTY issues with no work logged at all.
func (b *builder) worklogDate(c Clause) (string, error) {
	switch c.Op {
	case "is", "is not":
		if c.Op == "is" {
			return "NOT " + worklogExists + ")", nil
		}
		return worklogExists + ")", nil
	case "!=", "not in":
		pos := c
		pos.Op = map[string]string{"!=": "=", "not in": "in"}[c.Op]
		cond, err := b.dateField(pos, "w.started_at", false)
		if err != nil {
			return "", err
		}
		return "(NOT " + worklogExists + " AND " + cond + "))", nil
	}
	cond, err := b.dateField(c, "w.started_at", false)
	if err != nil {
		return "", err
	}
	return worklogExists + " AND " + cond + ")", nil
}

var relativePattern = regexp.MustCompile(`^([+-]?\d+)\s*([mhdwMy])$`)

// dateValue resolves a date literal, relative offset or date function.
// It returns the instant and whether the value denotes a whole day.
func (b *builder) dateValue(v Value) (time.Time, bool, error) {
	now := b.env.Now.UTC()
	day := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
	offset := func(t time.Time, s string) (time.Time, error) {
		m := relativePattern.FindStringSubmatch(strings.TrimSpace(s))
		if m == nil {
			return t, fmt.Errorf("bad offset %q", s)
		}
		n, _ := strconv.Atoi(m[1])
		switch m[2] {
		case "m":
			return t.Add(time.Duration(n) * time.Minute), nil
		case "h":
			return t.Add(time.Duration(n) * time.Hour), nil
		case "d":
			return t.AddDate(0, 0, n), nil
		case "w":
			return t.AddDate(0, 0, 7*n), nil
		case "M":
			return t.AddDate(0, n, 0), nil
		default:
			return t.AddDate(n, 0, 0), nil
		}
	}
	switch v.Kind {
	case VFunc:
		var t time.Time
		whole := false
		switch strings.ToLower(v.Text) {
		case "now":
			t = now
		case "startofday":
			t, whole = day, true
		case "endofday":
			t = day.AddDate(0, 0, 1).Add(-time.Nanosecond)
		case "startofweek":
			wd := (int(day.Weekday()) + 6) % 7 // Monday-based weeks
			t, whole = day.AddDate(0, 0, -wd), true
		case "endofweek":
			wd := (int(day.Weekday()) + 6) % 7
			t = day.AddDate(0, 0, 7-wd).Add(-time.Nanosecond)
		case "startofmonth":
			t, whole = time.Date(day.Year(), day.Month(), 1, 0, 0, 0, 0, time.UTC), true
		case "endofmonth":
			t = time.Date(day.Year(), day.Month()+1, 1, 0, 0, 0, 0, time.UTC).Add(-time.Nanosecond)
		case "startofyear":
			t, whole = time.Date(day.Year(), 1, 1, 0, 0, 0, 0, time.UTC), true
		case "endofyear":
			t = time.Date(day.Year()+1, 1, 1, 0, 0, 0, 0, time.UTC).Add(-time.Nanosecond)
		default:
			return time.Time{}, false, errAt(v.Pos, "unknown date function %s()", v.Text)
		}
		if len(v.Args) == 1 {
			s, err := b.str(v.Args[0])
			if err != nil {
				return time.Time{}, false, err
			}
			if t, err = offset(t, s); err != nil {
				return time.Time{}, false, errAt(v.Args[0].Pos, "%v", err)
			}
		}
		return t, whole, nil
	case VString, VNumber:
		s := strings.TrimSpace(v.Text)
		if relativePattern.MatchString(s) {
			t, err := offset(now, s)
			return t, false, err
		}
		for _, layout := range []string{"2006-01-02", "2006/01/02"} {
			if t, err := time.Parse(layout, s); err == nil {
				return t, true, nil
			}
		}
		for _, layout := range []string{"2006-01-02 15:04", "2006/01/02 15:04", time.RFC3339} {
			if t, err := time.Parse(layout, s); err == nil {
				return t.UTC(), false, nil
			}
		}
		return time.Time{}, false, errAt(v.Pos, "%q is not a date (use 2026-01-31, \"2026-01-31 14:00\", -7d or startOfWeek())", s)
	}
	return time.Time{}, false, errAt(v.Pos, "expected a date")
}

func (b *builder) dateField(c Clause, col string, dateOnly bool) (string, error) {
	cast := "::timestamptz"
	if dateOnly {
		cast = "::date"
	}
	param := func(t time.Time) string {
		if dateOnly {
			return b.arg(t.Format("2006-01-02")) + cast
		}
		return b.arg(t) + cast
	}
	switch c.Op {
	case "is", "is not":
		if c.Op == "is" {
			return col + " IS NULL", nil
		}
		return col + " IS NOT NULL", nil
	case ">", ">=", "<", "<=":
		t, _, err := b.dateValue(c.Values[0])
		if err != nil {
			return "", err
		}
		return fmt.Sprintf("%s %s %s", col, c.Op, param(t)), nil
	}
	return b.setField(c, func(v Value) (string, error) {
		t, whole, err := b.dateValue(v)
		if err != nil {
			return "", err
		}
		if whole && !dateOnly {
			return fmt.Sprintf("(%s >= %s AND %s < %s)", col, param(t), col, param(t.AddDate(0, 0, 1))), nil
		}
		return fmt.Sprintf("%s = %s", col, param(t)), nil
	}, col+" IS NULL")
}

func (b *builder) customClause(c Clause, f CustomField) (string, error) {
	key := b.arg(f.Key) + "::text"
	val := "i.fields->>" + key
	empty := "coalesce(" + val + ", '') = ''"
	switch f.Type {
	case "number":
		return b.numberField(c, "("+val+")::numeric")
	case "date":
		return b.dateField(c, "("+val+")::date", true)
	case "text", "url":
		return b.textField(c, val)
	case "checkbox":
		return b.setField(c, func(v Value) (string, error) {
			s, err := b.str(v)
			if err != nil {
				return "", err
			}
			on, err := strconv.ParseBool(s)
			if err != nil {
				return "", errAt(v.Pos, "%s is a checkbox; use true or false", f.Name)
			}
			return fmt.Sprintf("coalesce((%s)::boolean, false) = %s::boolean", val, b.arg(on)), nil
		}, empty)
	case "multiselect":
		return b.setField(c, func(v Value) (string, error) {
			s, err := b.str(v)
			if err != nil {
				return "", err
			}
			return fmt.Sprintf("jsonb_exists(coalesce(i.fields->%s, '[]'::jsonb), %s::text)", key, b.arg(s)), nil
		}, "jsonb_array_length(coalesce(i.fields->"+key+", '[]'::jsonb)) = 0")
	case "user":
		return b.setField(c, func(v Value) (string, error) {
			u, err := b.userSet(v)
			if err != nil {
				return "", err
			}
			return "(" + val + ")::uuid IN (" + u + ")", nil
		}, empty)
	case "version":
		return b.setField(c, func(v Value) (string, error) {
			s, err := b.str(v)
			if err != nil {
				return "", err
			}
			p := b.arg(s)
			return fmt.Sprintf("%s IN (SELECT id::text FROM releases WHERE lower(name) = lower(%[2]s::text) OR id::text = %[2]s::text)", val, p), nil
		}, empty)
	default: // select
		if c.Op == "~" || c.Op == "!~" {
			return b.textField(c, val)
		}
		return b.setField(c, func(v Value) (string, error) {
			s, err := b.str(v)
			if err != nil {
				return "", err
			}
			return fmt.Sprintf("lower(%s) = lower(%s::text)", val, b.arg(s)), nil
		}, empty)
	}
}

func orderExpr(o Order) (string, error) {
	switch normalize(o.Field) {
	case "rank":
		return "i.rank", nil
	case "created", "createddate":
		return "i.created_at", nil
	case "updated", "updateddate":
		return "i.updated_at", nil
	case "resolved", "resolutiondate":
		return "i.resolved_at", nil
	case "due", "duedate":
		return "i.due_date", nil
	case "start", "startdate":
		return "i.start_date", nil
	case "priority":
		return priorityRank, nil
	case "key", "issuekey":
		return "(SELECT key FROM projects p WHERE p.id = i.project_id), i.number", nil
	case "summary":
		return "lower(i.summary)", nil
	case "status":
		return "(SELECT CASE s.category WHEN 'todo' THEN 0 WHEN 'in_progress' THEN 1 ELSE 2 END FROM statuses s WHERE s.id = i.status_id)", nil
	case "assignee":
		return "(SELECT lower(u.display_name) FROM users u WHERE u.id = i.assignee_id)", nil
	case "storypoints", "points":
		return "i.story_points", nil
	case "type", "issuetype":
		return "(SELECT t.level FROM issue_types t WHERE t.id = i.type_id)", nil
	case "originalestimate", "timeoriginalestimate":
		return "i.original_estimate_seconds", nil
	case "remainingestimate", "timeestimate":
		return "i.remaining_estimate_seconds", nil
	case "timespent":
		return timeSpentExpr, nil
	case "timetofirstresponse", "firstresponse":
		return slaOrder("first_response"), nil
	case "timetoresolution":
		return slaOrder("resolution"), nil
	}
	return "", errAt(o.Pos, "cannot order by %q", o.Field)
}

// Fields describes the language for autocompletion (GET /search/fields).
var Fields = []struct {
	Name      string
	Type      string
	Operators []string
}{
	{"project", "project", []string{"=", "!=", "in", "not in"}},
	{"key", "issue", []string{"=", "!=", "in", "not in", ">", ">=", "<", "<="}},
	{"type", "type", []string{"=", "!=", "in", "not in"}},
	{"kind", "kind", []string{"=", "!=", "in", "not in"}},
	{"status", "status", []string{"=", "!=", "in", "not in"}},
	{"statusCategory", "category", []string{"=", "!=", "in", "not in"}},
	{"assignee", "user", []string{"=", "!=", "in", "not in", "is", "is not"}},
	{"reporter", "user", []string{"=", "!=", "in", "not in", "is", "is not"}},
	{"watcher", "user", []string{"=", "!=", "in", "is"}},
	{"priority", "priority", []string{"=", "!=", "in", "not in", ">", ">=", "<", "<="}},
	{"resolution", "resolution", []string{"=", "!=", "in", "not in", "is", "is not"}},
	{"labels", "label", []string{"=", "!=", "in", "not in", "is", "is not"}},
	{"sprint", "sprint", []string{"=", "!=", "in", "not in", "is", "is not"}},
	{"fixVersion", "release", []string{"=", "!=", "in", "not in", "is", "is not"}},
	{"deployedTo", "environment", []string{"=", "!=", "in", "not in", "is", "is not"}},
	{"parent", "issue", []string{"=", "!=", "in", "not in", "is", "is not"}},
	{"epic", "issue", []string{"=", "!=", "in", "not in", "is", "is not"}},
	{"issueLinkType", "linkType", []string{"=", "!=", "in", "not in", "is", "is not"}},
	{"level", "number", []string{"=", "!=", ">", ">=", "<", "<="}},
	{"summary", "text", []string{"~", "!~", "=", "!=", "is", "is not"}},
	{"description", "text", []string{"~", "!~", "is", "is not"}},
	{"text", "text", []string{"~", "!~"}},
	{"comment", "text", []string{"~", "!~"}},
	{"created", "date", []string{"=", ">", ">=", "<", "<="}},
	{"updated", "date", []string{"=", ">", ">=", "<", "<="}},
	{"resolved", "date", []string{"=", ">", ">=", "<", "<=", "is", "is not"}},
	{"due", "date", []string{"=", ">", ">=", "<", "<=", "is", "is not"}},
	{"start", "date", []string{"=", ">", ">=", "<", "<=", "is", "is not"}},
	{"storyPoints", "number", []string{"=", "!=", ">", ">=", "<", "<=", "is", "is not"}},
	{"originalEstimate", "duration", []string{"=", "!=", ">", ">=", "<", "<=", "is", "is not"}},
	{"remainingEstimate", "duration", []string{"=", "!=", ">", ">=", "<", "<=", "is", "is not"}},
	{"timeSpent", "duration", []string{"=", "!=", ">", ">=", "<", "<=", "is", "is not"}},
	{"worklogAuthor", "user", []string{"=", "!=", "in", "not in", "is", "is not"}},
	{"worklogDate", "date", []string{"=", "!=", ">", ">=", "<", "<=", "is", "is not"}},
	{"timeToFirstResponse", "sla", []string{"=", "!=", "<", "<=", ">", ">=", "is", "is not"}},
	{"timeToResolution", "sla", []string{"=", "!=", "<", "<=", ">", ">=", "is", "is not"}},
	{"requestType", "requestType", []string{"=", "!=", "in", "not in", "is", "is not"}},
	{"approval", "approval", []string{"=", "!=", "is", "is not"}},
	{"participant", "user", []string{"=", "!=", "in", "not in", "is", "is not"}},
}

var Functions = []string{
	"currentUser()", "membersOf(\"group\")", "openSprints()", "futureSprints()", "closedSprints()",
	"releasedVersions()", "unreleasedVersions()", "now()", "startOfDay()", "endOfDay()",
	"startOfWeek()", "endOfWeek()", "startOfMonth()", "endOfMonth()", "startOfYear()", "endOfYear()",
	"linkedIssues(\"KEY\")", "blockedIssues()", "blockingIssues()",
	"breached()", "running()", "paused()", "completed()", "remaining(\"2h\")",
	"pending()", "approved()", "declined()", "pendingBy(currentUser())",
}

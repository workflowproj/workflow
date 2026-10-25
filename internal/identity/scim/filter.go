package scim

import (
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	filter "github.com/scim2/filter-parser/v2"
)

// caseExact attributes compare case-sensitively (RFC 7643 2.2); everything else is case-insensitive.
var caseExact = map[string]bool{"id": true, "externalid": true}

func parseFilter(s string) (filter.Expression, error) {
	if strings.TrimSpace(s) == "" {
		return nil, nil
	}
	expr, err := filter.ParseFilter([]byte(s))
	if err != nil {
		return nil, errf(http.StatusBadRequest, "invalidFilter", "invalid filter %q: %v", s, err)
	}
	return expr, nil
}

// matches evaluates a SCIM filter against a resource rendered as JSON-like maps.
func matches(expr filter.Expression, res map[string]any) (bool, error) {
	switch e := expr.(type) {
	case nil:
		return true, nil
	case *filter.LogicalExpression:
		l, err := matches(e.Left, res)
		if err != nil {
			return false, err
		}
		if e.Operator == filter.AND && !l {
			return false, nil
		}
		if e.Operator == filter.OR && l {
			return true, nil
		}
		return matches(e.Right, res)
	case *filter.NotExpression:
		m, err := matches(e.Expression, res)
		return !m, err
	case *filter.ValuePath:
		for _, el := range asList(lookup(res, e.AttributePath.AttributeName, e.AttributePath.URIPrefix)) {
			m, ok := el.(map[string]any)
			if !ok {
				continue
			}
			if ok, err := matches(e.ValueFilter, m); err != nil || ok {
				return ok, err
			}
		}
		return false, nil
	case *filter.AttributeExpression:
		return matchAttr(res, e)
	}
	return false, errf(http.StatusBadRequest, "invalidFilter", "unsupported filter expression")
}

// lookup finds an attribute case-insensitively, honouring a schema URI prefix (extensions).
func lookup(res map[string]any, name string, uri *string) any {
	scope := res
	if uri != nil && *uri != SchemaUser && *uri != SchemaGroup {
		ext, _ := lookupKey(res, *uri).(map[string]any)
		if ext == nil {
			return nil
		}
		scope = ext
	}
	return lookupKey(scope, name)
}

func lookupKey(m map[string]any, name string) any {
	if v, ok := m[name]; ok {
		return v
	}
	for k, v := range m {
		if strings.EqualFold(k, name) {
			return v
		}
	}
	return nil
}

func asList(v any) []any {
	switch x := v.(type) {
	case nil:
		return nil
	case []any:
		return x
	case []map[string]any:
		out := make([]any, len(x))
		for i, e := range x {
			out[i] = e
		}
		return out
	}
	return []any{v}
}

func matchAttr(res map[string]any, e *filter.AttributeExpression) (bool, error) {
	path := e.AttributePath
	raw := lookup(res, path.AttributeName, path.URIPrefix)
	var values []any
	for _, v := range asList(raw) {
		m, isMap := v.(map[string]any)
		switch {
		case path.SubAttribute != nil && isMap:
			values = append(values, lookupKey(m, *path.SubAttribute))
		case path.SubAttribute != nil:
			// sub-attribute of a non-complex value: no match
		case isMap:
			values = append(values, lookupKey(m, "value")) // "emails eq x" means emails.value
		default:
			values = append(values, v)
		}
	}
	if e.Operator == filter.PR {
		for _, v := range values {
			if v != nil && v != "" {
				return true, nil
			}
		}
		return false, nil
	}
	exact := caseExact[strings.ToLower(path.AttributeName)]
	for _, v := range values {
		ok, err := compare(v, e.Operator, e.CompareValue, exact)
		if err != nil || ok {
			return ok, err
		}
	}
	// "ne" against a missing attribute is true.
	return e.Operator == filter.NE && len(values) == 0, nil
}

func compare(actual any, op filter.CompareOperator, want any, exact bool) (bool, error) {
	switch w := want.(type) {
	case bool:
		a, ok := toBool(actual)
		if !ok {
			return false, nil
		}
		switch op {
		case filter.EQ:
			return a == w, nil
		case filter.NE:
			return a != w, nil
		}
		return false, errf(http.StatusBadRequest, "invalidFilter", "operator %s is not valid for booleans", op)
	case int, float64:
		a, ok := toFloat(actual)
		wf, _ := toFloat(w)
		if !ok {
			return false, nil
		}
		switch op {
		case filter.EQ:
			return a == wf, nil
		case filter.NE:
			return a != wf, nil
		case filter.GT:
			return a > wf, nil
		case filter.GE:
			return a >= wf, nil
		case filter.LT:
			return a < wf, nil
		case filter.LE:
			return a <= wf, nil
		}
	case string:
		a := fmt.Sprint(actual)
		if actual == nil {
			a = ""
		}
		if t1, err1 := time.Parse(time.RFC3339, a); err1 == nil {
			if t2, err2 := time.Parse(time.RFC3339, w); err2 == nil {
				switch op {
				case filter.GT:
					return t1.After(t2), nil
				case filter.GE:
					return !t1.Before(t2), nil
				case filter.LT:
					return t1.Before(t2), nil
				case filter.LE:
					return !t1.After(t2), nil
				}
			}
		}
		if !exact {
			a, w = strings.ToLower(a), strings.ToLower(w)
		}
		switch op {
		case filter.EQ:
			return actual != nil && a == w, nil
		case filter.NE:
			return a != w, nil
		case filter.CO:
			return strings.Contains(a, w), nil
		case filter.SW:
			return strings.HasPrefix(a, w), nil
		case filter.EW:
			return strings.HasSuffix(a, w), nil
		case filter.GT:
			return a > w, nil
		case filter.GE:
			return a >= w, nil
		case filter.LT:
			return a < w, nil
		case filter.LE:
			return a <= w, nil
		}
	case nil:
		switch op {
		case filter.EQ:
			return actual == nil, nil
		case filter.NE:
			return actual != nil, nil
		}
	}
	return false, errf(http.StatusBadRequest, "invalidFilter", "unsupported comparison %s %v", op, want)
}

func toBool(v any) (bool, bool) {
	switch x := v.(type) {
	case bool:
		return x, true
	case string:
		b, err := strconv.ParseBool(x)
		return b, err == nil
	}
	return false, false
}

func toFloat(v any) (float64, bool) {
	switch x := v.(type) {
	case float64:
		return x, true
	case int:
		return float64(x), true
	case int64:
		return float64(x), true
	case string:
		f, err := strconv.ParseFloat(x, 64)
		return f, err == nil
	}
	return 0, false
}

// simpleEq recognises `attr eq "value"` so list endpoints can use an index instead of a scan.
func simpleEq(expr filter.Expression) (attr, value string, ok bool) {
	e, isAttr := expr.(*filter.AttributeExpression)
	if !isAttr || e.Operator != filter.EQ {
		return "", "", false
	}
	v, isStr := e.CompareValue.(string)
	if !isStr {
		return "", "", false
	}
	name := strings.ToLower(e.AttributePath.AttributeName)
	if e.AttributePath.SubAttribute != nil {
		name += "." + strings.ToLower(*e.AttributePath.SubAttribute)
	}
	return name, v, true
}

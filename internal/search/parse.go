// Package search implements a JQL-style query language that compiles to parameterised SQL.
//
//	project = PLAT AND status in ("To Do", "In Progress") AND assignee = currentUser()
//	type = Bug AND priority >= High AND created >= -7d ORDER BY priority DESC, created
//	text ~ "login" AND labels is EMPTY AND sprint in openSprints()
package search

import (
	"fmt"
	"strconv"
	"strings"
	"unicode"
)

// Error is a syntax or semantic error with the character offset it refers to.
type Error struct {
	Pos int
	Msg string
}

func (e *Error) Error() string { return fmt.Sprintf("%s (at position %d)", e.Msg, e.Pos) }

func errAt(pos int, format string, args ...any) *Error {
	return &Error{Pos: pos, Msg: fmt.Sprintf(format, args...)}
}

type tokenKind int

const (
	tEOF tokenKind = iota
	tIdent
	tString
	tNumber
	tOp
	tLParen
	tRParen
	tComma
)

type token struct {
	kind tokenKind
	text string // identifiers keep their case; keywords are compared case-insensitively
	pos  int
}

func lex(src string) ([]token, error) {
	var toks []token
	rs := []rune(src)
	i := 0
	for i < len(rs) {
		r := rs[i]
		switch {
		case unicode.IsSpace(r):
			i++
		case r == '(':
			toks = append(toks, token{tLParen, "(", i})
			i++
		case r == ')':
			toks = append(toks, token{tRParen, ")", i})
			i++
		case r == ',':
			toks = append(toks, token{tComma, ",", i})
			i++
		case r == '"' || r == '\'':
			start := i
			quote := r
			i++
			var b strings.Builder
			for {
				if i >= len(rs) {
					return nil, errAt(start, "unterminated string")
				}
				if rs[i] == '\\' && i+1 < len(rs) {
					b.WriteRune(rs[i+1])
					i += 2
					continue
				}
				if rs[i] == quote {
					i++
					break
				}
				b.WriteRune(rs[i])
				i++
			}
			toks = append(toks, token{tString, b.String(), start})
		case strings.ContainsRune("=!<>~", r):
			start := i
			op := string(r)
			if i+1 < len(rs) && (rs[i+1] == '=' || (r == '!' && rs[i+1] == '~')) {
				op += string(rs[i+1])
			}
			if op == "!" {
				return nil, errAt(start, "unexpected '!'")
			}
			i += len([]rune(op))
			toks = append(toks, token{tOp, op, start})
		default:
			start := i
			for i < len(rs) && !unicode.IsSpace(rs[i]) && !strings.ContainsRune("()=,!<>~\"'", rs[i]) {
				i++
			}
			if i == start {
				return nil, errAt(start, "unexpected character %q", r)
			}
			text := string(rs[start:i])
			kind := tIdent
			if _, err := strconv.ParseFloat(text, 64); err == nil {
				kind = tNumber
			}
			toks = append(toks, token{kind, text, start})
		}
	}
	return append(toks, token{tEOF, "", len(rs)}), nil
}

// ---------------------------------------------------------------------------
// AST

type Node interface{ node() }

type And struct{ Left, Right Node }
type Or struct{ Left, Right Node }
type Not struct{ X Node }

type Clause struct {
	Field  string
	Op     string // =, !=, >, >=, <, <=, ~, !~, in, not in, is, is not
	Values []Value
	Pos    int
}

func (And) node()    {}
func (Or) node()     {}
func (Not) node()    {}
func (Clause) node() {}

type ValueKind int

const (
	VString ValueKind = iota
	VNumber
	VFunc
	VEmpty
)

type Value struct {
	Kind ValueKind
	Text string // string / number text / function name
	Args []Value
	Pos  int
}

type Order struct {
	Field string
	Desc  bool
	Pos   int
}

type Query struct {
	Where   Node // nil when the query has no conditions
	OrderBy []Order
}

// ---------------------------------------------------------------------------
// Parser

type parser struct {
	toks []token
	i    int
}

func Parse(src string) (*Query, error) {
	toks, err := lex(src)
	if err != nil {
		return nil, err
	}
	p := &parser{toks: toks}
	q := &Query{}
	if !p.atKeyword("order") && p.peek().kind != tEOF {
		w, err := p.parseOr()
		if err != nil {
			return nil, err
		}
		q.Where = w
	}
	if p.atKeyword("order") {
		p.next()
		if !p.atKeyword("by") {
			return nil, errAt(p.peek().pos, "expected BY after ORDER")
		}
		p.next()
		for {
			t := p.next()
			if t.kind != tIdent && t.kind != tString {
				return nil, errAt(t.pos, "expected a field to order by")
			}
			o := Order{Field: t.text, Pos: t.pos}
			if p.atKeyword("asc") {
				p.next()
			} else if p.atKeyword("desc") {
				p.next()
				o.Desc = true
			}
			q.OrderBy = append(q.OrderBy, o)
			if p.peek().kind != tComma {
				break
			}
			p.next()
		}
	}
	if t := p.peek(); t.kind != tEOF {
		return nil, errAt(t.pos, "unexpected %q", t.text)
	}
	return q, nil
}

func (p *parser) peek() token { return p.toks[p.i] }

func (p *parser) next() token {
	t := p.toks[p.i]
	if p.i < len(p.toks)-1 {
		p.i++
	}
	return t
}

func (p *parser) atKeyword(kw string) bool {
	t := p.peek()
	return t.kind == tIdent && strings.EqualFold(t.text, kw)
}

func (p *parser) parseOr() (Node, error) {
	left, err := p.parseAnd()
	if err != nil {
		return nil, err
	}
	for p.atKeyword("or") {
		p.next()
		right, err := p.parseAnd()
		if err != nil {
			return nil, err
		}
		left = Or{left, right}
	}
	return left, nil
}

func (p *parser) parseAnd() (Node, error) {
	left, err := p.parseNot()
	if err != nil {
		return nil, err
	}
	for p.atKeyword("and") {
		p.next()
		right, err := p.parseNot()
		if err != nil {
			return nil, err
		}
		left = And{left, right}
	}
	return left, nil
}

func (p *parser) parseNot() (Node, error) {
	if p.atKeyword("not") {
		p.next()
		x, err := p.parseNot()
		if err != nil {
			return nil, err
		}
		return Not{x}, nil
	}
	return p.parsePrimary()
}

func (p *parser) parsePrimary() (Node, error) {
	t := p.peek()
	if t.kind == tLParen {
		p.next()
		n, err := p.parseOr()
		if err != nil {
			return nil, err
		}
		if c := p.next(); c.kind != tRParen {
			return nil, errAt(c.pos, "expected ')'")
		}
		return n, nil
	}
	return p.parseClause()
}

func (p *parser) parseClause() (Node, error) {
	f := p.next()
	if f.kind != tIdent && f.kind != tString {
		if f.kind == tEOF {
			return nil, errAt(f.pos, "expected a condition")
		}
		return nil, errAt(f.pos, "expected a field name, got %q", f.text)
	}
	c := Clause{Field: f.text, Pos: f.pos}
	op := p.next()
	switch {
	case op.kind == tOp:
		c.Op = op.text
		v, err := p.parseValue()
		if err != nil {
			return nil, err
		}
		c.Values = []Value{v}
	case op.kind == tIdent && strings.EqualFold(op.text, "in"):
		c.Op = "in"
		vs, err := p.parseList()
		if err != nil {
			return nil, err
		}
		c.Values = vs
	case op.kind == tIdent && strings.EqualFold(op.text, "not"):
		if !p.atKeyword("in") {
			return nil, errAt(p.peek().pos, "expected IN after NOT")
		}
		p.next()
		c.Op = "not in"
		vs, err := p.parseList()
		if err != nil {
			return nil, err
		}
		c.Values = vs
	case op.kind == tIdent && strings.EqualFold(op.text, "is"):
		c.Op = "is"
		if p.atKeyword("not") {
			p.next()
			c.Op = "is not"
		}
		v := p.next()
		if v.kind != tIdent || !(strings.EqualFold(v.text, "empty") || strings.EqualFold(v.text, "null")) {
			return nil, errAt(v.pos, "expected EMPTY after IS")
		}
		c.Values = []Value{{Kind: VEmpty, Pos: v.pos}}
	default:
		if op.kind == tEOF {
			return nil, errAt(op.pos, "expected an operator after %q", f.text)
		}
		return nil, errAt(op.pos, "expected an operator (=, !=, ~, in, is, ...) after %q, got %q", f.text, op.text)
	}
	return c, nil
}

func (p *parser) parseList() ([]Value, error) {
	// A single function call (e.g. `sprint in openSprints()`) is also accepted.
	if p.peek().kind == tIdent && p.toks[p.i+1].kind == tLParen {
		v, err := p.parseValue()
		return []Value{v}, err
	}
	open := p.next()
	if open.kind != tLParen {
		return nil, errAt(open.pos, "expected '(' to start a list")
	}
	var vs []Value
	for {
		v, err := p.parseValue()
		if err != nil {
			return nil, err
		}
		vs = append(vs, v)
		t := p.next()
		if t.kind == tRParen {
			return vs, nil
		}
		if t.kind != tComma {
			return nil, errAt(t.pos, "expected ',' or ')'")
		}
	}
}

func (p *parser) parseValue() (Value, error) {
	t := p.next()
	switch t.kind {
	case tString:
		return Value{Kind: VString, Text: t.text, Pos: t.pos}, nil
	case tNumber:
		return Value{Kind: VNumber, Text: t.text, Pos: t.pos}, nil
	case tIdent:
		if strings.EqualFold(t.text, "empty") || strings.EqualFold(t.text, "null") {
			return Value{Kind: VEmpty, Pos: t.pos}, nil
		}
		if p.peek().kind == tLParen {
			p.next()
			v := Value{Kind: VFunc, Text: t.text, Pos: t.pos}
			if p.peek().kind == tRParen {
				p.next()
				return v, nil
			}
			for {
				arg, err := p.parseValue()
				if err != nil {
					return Value{}, err
				}
				v.Args = append(v.Args, arg)
				c := p.next()
				if c.kind == tRParen {
					return v, nil
				}
				if c.kind != tComma {
					return Value{}, errAt(c.pos, "expected ',' or ')' in function arguments")
				}
			}
		}
		// Bare words (PLAT, High, -7d) are treated as strings.
		return Value{Kind: VString, Text: t.text, Pos: t.pos}, nil
	case tEOF:
		return Value{}, errAt(t.pos, "expected a value")
	}
	return Value{}, errAt(t.pos, "expected a value, got %q", t.text)
}

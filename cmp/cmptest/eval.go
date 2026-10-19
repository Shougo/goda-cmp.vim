package cmptest

import (
	"context"
	"fmt"
	"strconv"
	"strings"
)

// Eval evaluates a small part of Vim's expression language with the fakes in
// Funcs: function calls, lists, strings ('...' and "...") and integers, for
// instance "[bufname(3), getbufline(3, 1, '$')]". Anything else fails, as does
// a call to a function that Funcs lacks.
func (h *Host) Eval(_ context.Context, expr string) (any, error) {
	p := &parser{h: h, s: expr}
	v, err := p.value()
	if err != nil {
		return nil, fmt.Errorf("cmptest: eval %q: %w", expr, err)
	}
	p.space()
	if p.i != len(p.s) {
		return nil, fmt.Errorf("cmptest: eval %q: unexpected %q", expr, p.s[p.i:])
	}
	return v, nil
}

type parser struct {
	h *Host
	s string
	i int
}

func (p *parser) space() {
	for p.i < len(p.s) && (p.s[p.i] == ' ' || p.s[p.i] == '\t') {
		p.i++
	}
}

func (p *parser) peek() byte {
	p.space()
	if p.i < len(p.s) {
		return p.s[p.i]
	}
	return 0
}

func (p *parser) expect(c byte) error {
	if p.peek() != c {
		return fmt.Errorf("want %q at %d", c, p.i)
	}
	p.i++
	return nil
}

func (p *parser) value() (any, error) {
	switch c := p.peek(); {
	case c == '[':
		p.i++
		items, err := p.list(']')
		if err != nil {
			return nil, err
		}
		return items, nil
	case c == '\'':
		return p.single()
	case c == '"':
		return p.double()
	case c == '-' || c >= '0' && c <= '9':
		j := p.i + 1
		for j < len(p.s) && p.s[j] >= '0' && p.s[j] <= '9' {
			j++
		}
		n, err := strconv.ParseInt(p.s[p.i:j], 10, 64)
		p.i = j
		return n, err
	case c == '_' || c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z':
		j := p.i
		for j < len(p.s) && (strings.IndexByte("_#:.", p.s[j]) >= 0 ||
			p.s[j] >= 'a' && p.s[j] <= 'z' || p.s[j] >= 'A' && p.s[j] <= 'Z' || p.s[j] >= '0' && p.s[j] <= '9') {
			j++
		}
		name := p.s[p.i:j]
		p.i = j
		if err := p.expect('('); err != nil {
			return nil, fmt.Errorf("%s: only function calls are supported", name)
		}
		args, err := p.list(')')
		if err != nil {
			return nil, err
		}
		return p.h.Call(context.Background(), name, args...)
	}
	return nil, fmt.Errorf("unexpected input at %d", p.i)
}

// list reads values separated by commas up to end (already past the opener).
func (p *parser) list(end byte) ([]any, error) {
	out := []any{}
	if p.peek() == end {
		p.i++
		return out, nil
	}
	for {
		v, err := p.value()
		if err != nil {
			return nil, err
		}
		out = append(out, v)
		switch p.peek() {
		case ',':
			p.i++
		case end:
			p.i++
			return out, nil
		default:
			return nil, fmt.Errorf("want ',' or %q at %d", end, p.i)
		}
	}
}

func (p *parser) single() (string, error) {
	var b strings.Builder
	for p.i++; p.i < len(p.s); p.i++ {
		if p.s[p.i] == '\'' {
			if p.i+1 < len(p.s) && p.s[p.i+1] == '\'' {
				b.WriteByte('\'')
				p.i++
				continue
			}
			p.i++
			return b.String(), nil
		}
		b.WriteByte(p.s[p.i])
	}
	return "", fmt.Errorf("unterminated string")
}

func (p *parser) double() (string, error) {
	j := p.i + 1
	for j < len(p.s) && p.s[j] != '"' {
		if p.s[j] == '\\' {
			j++
		}
		j++
	}
	if j >= len(p.s) {
		return "", fmt.Errorf("unterminated string")
	}
	s, err := strconv.Unquote(p.s[p.i : j+1])
	p.i = j + 1
	return s, err
}

// Package converterremoveoverlap is a converter that removes from a
// candidate the part the text after the cursor already has: completing
// "foobar" before "bar" inserts "foo". A candidate with a parenthesis that
// the text after the cursor closes is left alone. It is a port of
// ddc-filter-converter_remove_overlap
// (https://github.com/Shougo/ddc-filter-converter_remove_overlap).
package converterremoveoverlap

import (
	"context"
	"regexp"
	"strings"

	"github.com/Shougo/goda-cmp.vim/cmp"
)

type filter struct{}

// New returns the filter, named "converterremoveoverlap".
func New() cmp.Filter { return filter{} }

func (filter) Name() string { return "converterremoveoverlap" }

var token = regexp.MustCompile(`[a-zA-Z_]\w*|\W`)

// tokens splits as the original's split(/([a-zA-Z_]\w*|\W)/): the words,
// the single non-word characters and what is between them.
func tokens(s string) []string {
	var out []string
	at := 0
	for _, loc := range token.FindAllStringIndex(s, -1) {
		if loc[0] > at {
			out = append(out, s[at:loc[0]])
		}
		out = append(out, s[loc[0]:loc[1]])
		at = loc[1]
	}
	if at < len(s) {
		out = append(out, s[at:])
	}
	return out
}

// overlapLength is the length of the longest run of the next words that
// left ends with.
func overlapLength(left string, next []string) int {
	joined := strings.Join(next, "")
	n := len(joined)
	for i := len(next) - 1; i >= 0; i-- {
		if strings.HasSuffix(left, joined[:n]) {
			return n
		}
		n -= len(next[i])
	}
	return 0
}

func (filter) Filter(ctx context.Context, in cmp.FilterInput) ([]cmp.Item, error) {
	if in.Context == nil || in.Context.NextInput == "" {
		return in.Items, nil
	}
	next := tokens(in.Context.NextInput)
	if len(next) == 0 {
		return in.Items, nil
	}
	// Pairs that the text after the cursor closes on this line.
	var pairs [][2]string
	if in.Host != nil {
		for _, p := range [][2]string{{"(", ")"}, {"[", "]"}} {
			if closes(ctx, in, p[0], p[1]) {
				pairs = append(pairs, p)
			}
		}
	}
	out := append([]cmp.Item(nil), in.Items...)
	for i, it := range out {
		w := it.Word
		ov := overlapLength(w, next)
		if ov == 0 {
			continue
		}
		skip := false
		for _, p := range pairs {
			if strings.Contains(w, p[0]) && strings.Contains(w[:len(w)-ov], p[1]) {
				skip = true
				break
			}
		}
		if skip {
			continue
		}
		if out[i].Abbr == "" {
			out[i].Abbr = w
		}
		out[i].Word = w[:len(w)-ov]
	}
	return out, nil
}

// closes reports whether the pair begin..end opened before the cursor is
// closed after it, on the same line.
func closes(ctx context.Context, in cmp.FilterInput, begin, end string) bool {
	if !strings.Contains(in.Context.Input, begin) {
		return false
	}
	v, err := in.Host.Eval(ctx, "[getcurpos()[1:2], searchpairpos('\\V"+begin+"', '', '\\V"+end+"', 'nW', '', line('.'))]")
	if err != nil {
		return false
	}
	l, _ := v.([]any)
	if len(l) != 2 {
		return false
	}
	cur, pair := ints(l[0]), ints(l[1])
	if len(cur) != 2 || len(pair) != 2 || pair[0] == 0 {
		return false
	}
	return cur[0] == pair[0] && cur[1] < pair[1]
}

func ints(v any) []int {
	l, _ := v.([]any)
	out := make([]int, 0, len(l))
	for _, x := range l {
		switch n := x.(type) {
		case int64:
			out = append(out, int(n))
		case uint64:
			out = append(out, int(n))
		case int:
			out = append(out, n)
		case int8:
			out = append(out, int(n))
		case float64:
			out = append(out, int(n))
		}
	}
	return out
}

// Package input is a completion source for input() and the expression
// register: what getcompletion() offers for the completion type of input()
// ("@"), and expressions for "=". It is a port of ddc-source-input
// (https://github.com/Shougo/ddc-source-input).
//
//	CmdlineSources: map[string][]string{"@": {"input"}, "=": {"input"}},
//
// goda_cmp#enable_cmdline() does not complete "=" (the expression register);
// "@" works when input() was called with a completion type.
package input

import (
	"context"
	"fmt"
	"strings"

	"github.com/Shougo/goda-cmp.vim/cmp"
	"github.com/Shougo/goda.vim/sdk"
)

type source struct{}

// New returns the source, named "input".
func New() cmp.Source { return source{} }

func (source) Name() string { return "input" }

func (source) Gather(ctx context.Context, h sdk.Host, c *cmp.Context) (cmp.Gathered, error) {
	if c.CmdType != "=" && c.CmdType != "@" {
		return cmp.Gathered{Start: len(c.Input)}, nil
	}
	typ := "expression"
	if c.CmdType == "@" {
		typ = ""
		if v, err := h.Call(ctx, "getcmdcompltype"); err == nil { // not in old editors
			typ, _ = v.(string)
		}
		if typ == "" {
			return cmp.Gathered{Start: len(c.Input)}, nil
		}
	}
	var results []string
	if v, err := h.Call(ctx, "getcompletion", c.Input, typ); err == nil { // errors are ignored, as in the original
		if l, ok := v.([]any); ok {
			for _, x := range l {
				results = append(results, fmt.Sprint(x))
			}
		}
	}
	if len(results) == 0 {
		return cmp.Gathered{Start: 0, Incomplete: true}, nil
	}
	// From the head of the input (completeStr is all of it): the common
	// prefix the input ends with is removed from the results.
	prefix := []rune(strings.ToLower(results[0]))
	for _, w := range results {
		for !strings.HasPrefix(strings.ToLower(w), string(prefix)) {
			prefix = prefix[:len(prefix)-1]
		}
	}
	in := strings.ToLower(c.Input)
	for !strings.HasSuffix(in, string(prefix)) {
		prefix = prefix[:len(prefix)-1]
	}
	if n := len(prefix) - len([]rune(c.Input)); len(prefix) > 0 && string(prefix) != c.Input && n > 0 {
		for i, w := range results {
			r := []rune(w)
			results[i] = string(r[min(n, len(r)):])
		}
	}
	items := make([]cmp.Item, 0, len(results))
	for _, w := range results {
		if strings.HasSuffix(w, "/") {
			items = append(items, cmp.Item{Word: strings.TrimSuffix(w, "/"), Abbr: w})
		} else {
			items = append(items, cmp.Item{Word: w})
		}
	}
	// getcompletion() filters by all of the input.
	return cmp.Gathered{Start: 0, Items: items, Incomplete: true}, nil
}

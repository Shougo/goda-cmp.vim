// Package cmdlinehistory is a completion source for the command line: the
// entries of the command-line history (histget()) of the current type. It is
// a port of ddc-source-cmdline_history
// (https://github.com/Shougo/ddc-source-cmdline-history).
//
// While the command line has no space, an entry is offered whole; after one,
// entries that start with the same text are offered from the word under the
// cursor on. Use it in Config.CmdlineSources:
//
//	b.AddSource(cmdlinehistory.New(cmdlinehistory.Params{}))
//	b.PatchGlobal(cmp.Config{
//		CmdlineSources: map[string][]string{
//			":": {"cmdline", "cmdlinehistory"},
//			"/": {"cmdlinehistory", "around"},
//		},
//		SourceOptions: map[string]cmp.SourceOption{
//			"cmdlinehistory": {Mark: "history"},
//		},
//	})
//
// It completes nothing in insert mode.
package cmdlinehistory

import (
	"context"
	"fmt"
	"strings"

	"github.com/Shougo/goda-cmp.vim/cmp"
	"github.com/Shougo/goda.vim/sdk"
)

// Params are the parameters of the source.
type Params struct {
	// Limit is how many entries, from the newest, are looked at. Default
	// 1000.
	Limit int
}

type source struct{ limit int }

// New returns the source, named "cmdlinehistory".
func New(p Params) cmp.Source {
	if p.Limit <= 0 {
		p.Limit = 1000
	}
	return source{limit: p.Limit}
}

func (source) Name() string { return "cmdlinehistory" }

func (s source) Gather(ctx context.Context, h sdk.Host, c *cmp.Context) (cmp.Gathered, error) {
	if c.Mode != "c" {
		return cmp.Gathered{Start: len(c.Input)}, nil
	}
	hist, err := History(ctx, h, c.CmdType, s.limit)
	if err != nil {
		return cmp.Gathered{}, err
	}
	start := 0
	if strings.Contains(c.Input, " ") {
		start = c.KeywordStart()
	}
	prefix := c.Input[:start]
	items := make([]cmp.Item, 0, len(hist))
	for _, w := range hist {
		if !strings.HasPrefix(w, prefix) || strings.ContainsAny(w, "\r\n") || len(w) == start {
			continue
		}
		items = append(items, cmp.Item{Word: w[start:]})
	}
	return cmp.Gathered{Start: start, Items: items}, nil
}

// History returns at most limit entries of the history of the command-line
// type cmdType (getcmdtype()), the newest first. When the command line
// completes directories or files (getcmdcompltype()), only entries that
// name an existing one are kept, as ddc-source-cmdline_history does.
func History(ctx context.Context, h sdk.Host, cmdType string, limit int) ([]string, error) {
	if cmdType == "" {
		return nil, nil
	}
	v, err := h.Call(ctx, "histnr", cmdType)
	if err != nil {
		return nil, err
	}
	n := min(toInt(v), limit)
	if n < 1 {
		return nil, nil
	}
	idx := make([]any, n)
	for i := range idx {
		idx[i] = -(i + 1)
	}
	// One call for all the entries: map() over the indexes.
	v, err = h.Call(ctx, "map", idx, "histget("+vimString(cmdType)+", v:val)")
	if err != nil {
		return nil, err
	}
	list := toStrings(v)

	compltype := ""
	if v, err := h.Call(ctx, "getcmdcompltype"); err == nil { // not in old editors
		compltype, _ = v.(string)
	}
	var keep string
	switch compltype {
	case "dir":
		keep = "isdirectory(v:val)"
	case "file":
		keep = "isdirectory(v:val) || filereadable(v:val)"
	}
	if keep != "" && len(list) > 0 {
		v, err := h.Call(ctx, "filter", toAny(list), keep)
		if err != nil {
			return nil, err
		}
		list = toStrings(v)
	}
	// Empty entries say nothing.
	out := list[:0]
	for _, w := range list {
		if w != "" {
			out = append(out, w)
		}
	}
	return out, nil
}

// vimString quotes s as a Vim string literal.
func vimString(s string) string { return "'" + strings.ReplaceAll(s, "'", "''") + "'" }

func toInt(v any) int {
	switch n := v.(type) {
	case int:
		return n
	case int64:
		return int(n)
	case uint64:
		return int(n)
	case int8:
		return int(n)
	case float64:
		return int(n)
	}
	return 0
}

func toStrings(v any) []string {
	switch l := v.(type) {
	case []string:
		return l
	case []any:
		out := make([]string, 0, len(l))
		for _, x := range l {
			out = append(out, fmt.Sprint(x))
		}
		return out
	}
	return nil
}

func toAny(l []string) []any {
	out := make([]any, len(l))
	for i, s := range l {
		out[i] = s
	}
	return out
}

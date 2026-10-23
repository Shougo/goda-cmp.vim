// Package sortershellhistory is a sorter that puts first the candidates
// found in a recent command of a shell history file, the newest first. It is a port of
// ddc-filter-sorter_shell_history
// (https://github.com/Shougo/ddc-source-shell_history).
package sortershellhistory

import (
	"context"
	"slices"
	"strings"

	"github.com/Shougo/goda-cmp.vim/cmp"
	"github.com/Shougo/goda-cmp.vim/ext/source/shellhistory"
)

// Params are the parameters of the filter.
type Params struct {
	// Paths are the history files; "~" and environment variables are
	// expanded.
	Paths []string `json:"paths"`
	// Limit is how many commands of each file are looked at (default 500).
	Limit int `json:"limit"`
}

type filter struct{ p Params }

// New returns the filter, named "sortershellhistory".
func New(p Params) cmp.Filter {
	if p.Limit <= 0 {
		p.Limit = 500
	}
	return filter{p}
}

func (filter) Name() string { return "sortershellhistory" }

// WithParams sets the parameters from Vim script (filterParams).
func (f filter) WithParams(p cmp.Params) (any, error) {
	np, err := cmp.PatchParams(f.p, p)
	if err != nil {
		return nil, err
	}
	return New(np), nil
}

func (f filter) Filter(_ context.Context, in cmp.FilterInput) ([]cmp.Item, error) {
	var hist []string
	for _, p := range f.p.Paths {
		hist = append(hist, shellhistory.History(shellhistory.Expand(p), f.p.Limit)...)
	}
	scores := map[string]int{}
	for _, it := range in.Items {
		if _, ok := scores[it.Word]; !ok {
			scores[it.Word] = score(f.p.Limit, it.Word, hist)
		}
	}
	out := slices.Clone(in.Items)
	slices.SortStableFunc(out, func(a, b cmp.Item) int { return scores[a.Word] - scores[b.Word] })
	return out, nil
}

// score is the index of the first entry that contains word, or def.
func score(def int, word string, hist []string) int {
	for i, h := range hist {
		if strings.Contains(h, word) {
			return i
		}
	}
	return def
}

// Package sortercmdlinehistory is a sorter for the command line: candidates
// found in a recent history entry come first, in the order of the entries
// (newest first). It is a port of ddc-filter-sorter_cmdline_history
// (https://github.com/Shougo/ddc-source-cmdline-history).
//
//	b.AddFilter(sortercmdlinehistory.New(sortercmdlinehistory.Params{}))
//	b.PatchGlobal(cmp.Config{
//		SourceOptions: map[string]cmp.SourceOption{
//			"cmdline": {Sorters: []string{"sortercmdlinehistory"}},
//		},
//	})
//
// Outside the command line it leaves the order alone.
package sortercmdlinehistory

import (
	"context"
	"slices"
	"strings"

	"github.com/Shougo/goda-cmp.vim/cmp"
	"github.com/Shougo/goda-cmp.vim/ext/source/cmdlinehistory"
)

// Params are the parameters of the filter.
type Params struct {
	// Limit is how many history entries, from the newest, are looked at.
	// Default 500.
	Limit int `json:"limit"`
}

type filter struct{ limit int }

// New returns the filter, named "sortercmdlinehistory".
func New(p Params) cmp.Filter {
	if p.Limit <= 0 {
		p.Limit = 500
	}
	return filter{limit: p.Limit}
}

func (filter) Name() string { return "sortercmdlinehistory" }

// WithParams sets the parameters from Vim script (filterParams).
func (f filter) WithParams(p cmp.Params) (any, error) {
	np, err := cmp.PatchParams(Params{Limit: f.limit}, p)
	if err != nil {
		return nil, err
	}
	return New(np), nil
}

func (f filter) Filter(ctx context.Context, in cmp.FilterInput) ([]cmp.Item, error) {
	if in.Host == nil || in.Context == nil || in.Context.Mode != "c" {
		return in.Items, nil
	}
	hist, err := cmdlinehistory.History(ctx, in.Host, in.Context.CmdType, f.limit)
	if err != nil {
		return nil, err
	}
	scores := map[string]int{}
	for _, it := range in.Items {
		if _, ok := scores[it.Word]; !ok {
			scores[it.Word] = score(f.limit, it.Word, hist)
		}
	}
	out := slices.Clone(in.Items)
	slices.SortStableFunc(out, func(a, b cmp.Item) int { return scores[a.Word] - scores[b.Word] })
	return out, nil
}

// score is the index of the first (newest) entry that contains word, or
// def.
func score(def int, word string, hist []string) int {
	for i, h := range hist {
		if strings.Contains(h, word) {
			return i
		}
	}
	return def
}

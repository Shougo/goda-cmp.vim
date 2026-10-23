// Package convertertruncateabbr is a converter that shortens long labels so
// the popup stays narrow.
package convertertruncateabbr

import (
	"context"

	"github.com/Shougo/goda-cmp.vim/cmp"
)

// Params are the settings of the filter.
type Params struct {
	// MaxWidth is the longest label in characters (default 40).
	MaxWidth int `json:"maxWidth"`
}

type filter struct {
	p   Params
	max int
}

// New returns the filter, named "convertertruncateabbr".
func New(p Params) cmp.Filter {
	if p.MaxWidth <= 1 {
		p.MaxWidth = 40
	}
	return filter{p, p.MaxWidth}
}

func (filter) Name() string { return "convertertruncateabbr" }

// WithParams sets the parameters from Vim script (filterParams).
func (f filter) WithParams(p cmp.Params) (any, error) {
	np, err := cmp.PatchParams(f.p, p)
	if err != nil {
		return nil, err
	}
	return New(np), nil
}

func (f filter) Filter(_ context.Context, in cmp.FilterInput) ([]cmp.Item, error) {
	out := append([]cmp.Item(nil), in.Items...)
	for i, it := range out {
		if r := []rune(it.Display()); len(r) > f.max {
			out[i].Abbr = string(r[:f.max-1]) + "…"
		}
	}
	return out, nil
}

// Package truncateabbr is a converter that shortens long labels so the popup
// stays narrow.
package truncateabbr

import (
	"context"

	"github.com/Shougo/goda-cmp.vim/cmp"
)

// Params are the settings of the filter.
type Params struct {
	// MaxWidth is the longest label in characters (default 40).
	MaxWidth int
}

type filter struct{ max int }

// New returns the filter, named "converter_truncate_abbr".
func New(p Params) cmp.Filter {
	if p.MaxWidth <= 1 {
		p.MaxWidth = 40
	}
	return filter{p.MaxWidth}
}

func (filter) Name() string { return "converter_truncate_abbr" }

func (f filter) Filter(_ context.Context, in cmp.FilterInput) ([]cmp.Item, error) {
	out := append([]cmp.Item(nil), in.Items...)
	for i, it := range out {
		if r := []rune(it.Display()); len(r) > f.max {
			out[i].Abbr = string(r[:f.max-1]) + "…"
		}
	}
	return out, nil
}

// Package matcherlength is a matcher that keeps the candidates longer than
// the typed text, so that what is typed already is not offered. It is a port
// of ddc-filter-matcher_length
// (https://github.com/Shougo/ddc-filter-matcher_length).
package matcherlength

import (
	"context"
	"unicode/utf8"

	"github.com/Shougo/goda-cmp.vim/cmp"
)

type filter struct{}

// New returns the filter, named "matcherlength".
func New() cmp.Filter { return filter{} }

func (filter) Name() string { return "matcherlength" }

func (filter) Filter(_ context.Context, in cmp.FilterInput) ([]cmp.Item, error) {
	n := utf8.RuneCountInString(in.Complete)
	out := make([]cmp.Item, 0, len(in.Items))
	for _, it := range in.Items {
		if utf8.RuneCountInString(it.Word) > n {
			out = append(out, it)
		}
	}
	return out, nil
}

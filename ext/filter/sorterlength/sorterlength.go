// Package sorterlength is a sorter that puts shorter candidates first.
package sorterlength

import (
	"context"
	"sort"
	"unicode/utf8"

	"github.com/Shougo/goda-cmp.vim/cmp"
)

type filter struct{}

// New returns the filter, named "sorter_length".
func New() cmp.Filter { return filter{} }

func (filter) Name() string { return "sorter_length" }

func (filter) Filter(_ context.Context, in cmp.FilterInput) ([]cmp.Item, error) {
	out := append([]cmp.Item(nil), in.Items...)
	sort.SliceStable(out, func(i, j int) bool {
		return utf8.RuneCountInString(out[i].Word) < utf8.RuneCountInString(out[j].Word)
	})
	return out, nil
}

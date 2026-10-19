// Package sortertext is a sorter that orders candidates by Item.SortText
// (the LSP sortText), falling back to the word.
package sortertext

import (
	"context"
	"sort"

	"github.com/Shougo/goda-cmp.vim/cmp"
)

type filter struct{}

// New returns the filter, named "sorter_text".
func New() cmp.Filter { return filter{} }

func (filter) Name() string { return "sorter_text" }

func (filter) Filter(_ context.Context, in cmp.FilterInput) ([]cmp.Item, error) {
	out := append([]cmp.Item(nil), in.Items...)
	sort.SliceStable(out, func(i, j int) bool { return out[i].Sort() < out[j].Sort() })
	return out, nil
}

// Package sorterhead is a sorter that moves up the candidates that are the
// head of longer candidates further down, so that the shortest of a family
// comes first. It is a port of ddc-filter-sorter_head
// (https://github.com/Shougo/ddc-filter-sorter_head), meant for postFilters.
package sorterhead

import (
	"context"
	"sort"
	"strings"
	"unicode/utf8"

	"github.com/Shougo/goda-cmp.vim/cmp"
)

type filter struct{}

// New returns the filter, named "sorterhead".
func New() cmp.Filter { return filter{} }

func (filter) Name() string { return "sorterhead" }

func (filter) Filter(_ context.Context, in cmp.FilterInput) ([]cmp.Item, error) {
	words := make([]string, len(in.Items))
	for i, it := range in.Items {
		words[i] = it.Word
	}
	type scored struct {
		it    cmp.Item
		index int
		score float64
	}
	ds := make([]scored, len(in.Items))
	for i, it := range in.Items {
		ds[i] = scored{it, i, score(in.Complete, it.Word, words, i)}
	}
	sort.SliceStable(ds, func(a, b int) bool {
		if ds[a].score != ds[b].score {
			return ds[a].score < ds[b].score
		}
		return ds[a].index < ds[b].index
	})
	out := make([]cmp.Item, len(ds))
	for i, d := range ds {
		out[i] = d.it
	}
	return out, nil
}

// score is the original's calcScore: an item gains for every longer item it
// is the head of, more for the earlier ones.
func score(input, str string, words []string, self int) float64 {
	s := float64(self)
	minLength := utf8.RuneCountInString(input) + 2
	for i, w := range words {
		if utf8.RuneCountInString(w) > minLength && strings.HasPrefix(w, str) {
			s -= float64(len(words)-i) / 2
			if s < 0 {
				s = 0
			}
		}
	}
	return s
}

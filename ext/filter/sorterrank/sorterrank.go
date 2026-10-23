// Package sorterrank is a sorter that ranks candidates by how they match the
// typed text (a prefix, a prefix ignoring case, a part ignoring case), by
// length, and by how near the cursor they occur in the buffer. It is a port
// of ddc-filter-sorter_rank (https://github.com/Shougo/ddc-filter-sorter_rank).
package sorterrank

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"sync"
	"unicode"
	"unicode/utf8"

	"github.com/Shougo/goda-cmp.vim/cmp"
)

const (
	linesMax   = 150 // lines around the cursor looked at
	columnsMax = 150 // longer lines are skipped
)

type filter struct {
	mu    *sync.Mutex
	key   *string        // bufnr and line the cache is for
	cache map[string]int // word -> the nearest line it occurs on
}

// New returns the filter, named "sorterrank".
func New() cmp.Filter {
	return filter{mu: &sync.Mutex{}, key: new(string), cache: map[string]int{}}
}

func (filter) Name() string { return "sorterrank" }

func (f filter) Filter(ctx context.Context, in cmp.FilterInput) ([]cmp.Item, error) {
	if in.Complete == "" || in.Context == nil {
		return in.Items, nil
	}
	lnum := in.Context.Lnum
	cache := f.words(ctx, in)
	scores := make(map[string]int, len(in.Items))
	for _, it := range in.Items {
		if _, ok := scores[it.Word]; !ok {
			scores[it.Word] = score(it.Word, in.Complete, cache, lnum)
		}
	}
	out := append([]cmp.Item(nil), in.Items...)
	sort.SliceStable(out, func(a, b int) bool { return scores[out[a].Word] > scores[out[b].Word] })
	return out, nil
}

// words returns the words around the cursor. The original collects them on
// InsertEnter; here, once per buffer and line of the cursor.
func (f filter) words(ctx context.Context, in cmp.FilterInput) map[string]int {
	c := in.Context
	key := fmt.Sprint(c.Bufnr, ":", c.Lnum, ":", c.Mode)
	f.mu.Lock()
	defer f.mu.Unlock()
	if *f.key == key || in.Host == nil {
		return f.cache
	}
	first := max(1, c.Lnum-linesMax)
	v, err := in.Host.Call(ctx, "getbufline", c.Bufnr, first, c.Lnum+linesMax)
	if err != nil {
		return f.cache
	}
	lines, _ := v.([]any)
	cache := map[string]int{}
	for i, l := range lines {
		line, _ := l.(string)
		if utf8.RuneCountInString(line) > columnsMax {
			continue
		}
		lnr := first + i
		for _, w := range keywords(line) {
			if old, ok := cache[w]; ok && abs(old-c.Lnum) <= abs(lnr-c.Lnum) {
				continue
			}
			cache[w] = lnr
		}
	}
	*f.key = key
	for k := range f.cache {
		delete(f.cache, k)
	}
	for k, v := range cache {
		f.cache[k] = v
	}
	return f.cache
}

// score is the original's calcScore.
func score(str, complete string, cache map[string]int, lnum int) int {
	s := 0
	lstr, lcomplete := strings.ToLower(str), strings.ToLower(complete)
	switch {
	case strings.HasPrefix(str, complete):
		s += 200
	case strings.HasPrefix(lstr, lcomplete):
		s += 60
	case strings.Index(lstr, lcomplete) > 0:
		s += 20
	}
	s -= utf8.RuneCountInString(str) * 2
	if l, ok := cache[str]; ok {
		s += linesMax - abs(l-lnum)
	}
	return s
}

// keywords are the words of a line ('iskeyword' approximated by letters,
// digits and "_").
func keywords(line string) []string {
	return strings.FieldsFunc(line, func(r rune) bool {
		return !(r == '_' || unicode.IsLetter(r) || unicode.IsDigit(r))
	})
}

func abs(n int) int {
	if n < 0 {
		return -n
	}
	return n
}

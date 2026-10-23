// Package matcherprefix is a matcher that keeps the candidates starting with
// the first characters typed (PrefixLength, default 1), and those that start
// with a non-word character. It is a port of ddc-filter-matcher_prefix
// (https://github.com/Shougo/ddc-filter-matcher_prefix). Case matters.
package matcherprefix

import (
	"context"
	"strings"

	"github.com/Shougo/goda-cmp.vim/cmp"
)

// Params are the parameters of the filter.
type Params struct {
	// PrefixLength is how many typed characters must match (default 1; 0
	// keeps everything).
	PrefixLength *int `json:"prefixLength"`
}

type filter struct{ p Params }

// New returns the filter, named "matcherprefix".
func New(p Params) cmp.Filter { return filter{p} }

func (filter) Name() string { return "matcherprefix" }

// WithParams sets the parameters from Vim script (filterParams).
func (f filter) WithParams(p cmp.Params) (any, error) {
	np, err := cmp.PatchParams(f.p, p)
	if err != nil {
		return nil, err
	}
	return New(np), nil
}

func (f filter) Filter(_ context.Context, in cmp.FilterInput) ([]cmp.Item, error) {
	n := 1
	if f.p.PrefixLength != nil {
		n = max(0, *f.p.PrefixLength)
	}
	r := []rune(in.Complete)
	prefix := string(r[:min(n, len(r))])
	out := make([]cmp.Item, 0, len(in.Items))
	for _, it := range in.Items {
		// A source may offer items that do not start with a word ("-option").
		if startsNonWord(it.Word) || strings.HasPrefix(it.Word, prefix) {
			out = append(out, it)
		}
	}
	return out, nil
}

// startsNonWord is JavaScript's /^\W/.
func startsNonWord(s string) bool {
	if s == "" {
		return false
	}
	c := s[0]
	return !(c == '_' || c >= '0' && c <= '9' || c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z')
}

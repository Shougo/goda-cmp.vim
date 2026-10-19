// Package matcherhead is a matcher that keeps the candidates starting with
// the typed text.
package matcherhead

import (
	"context"
	"strings"
	"unicode"

	"github.com/Shougo/goda-cmp.vim/cmp"
)

// Params are the settings of the filter. The zero value ignores case, except
// that a typed capital letter makes the match case sensitive (smartcase).
type Params struct {
	// CaseSensitive turns the case folding off completely.
	CaseSensitive bool
}

type filter struct{ p Params }

// New returns the filter, named "matcher_head".
func New(p Params) cmp.Filter { return filter{p} }

func (filter) Name() string { return "matcher_head" }

func (f filter) Filter(_ context.Context, in cmp.FilterInput) ([]cmp.Item, error) {
	typed := in.Complete
	fold := !f.p.CaseSensitive && !strings.ContainsFunc(typed, unicode.IsUpper)
	if fold {
		typed = strings.ToLower(typed)
	}
	out := make([]cmp.Item, 0, len(in.Items))
	for _, it := range in.Items {
		w := it.Filter()
		if fold {
			w = strings.ToLower(w)
		}
		if strings.HasPrefix(w, typed) {
			out = append(out, it)
		}
	}
	return out, nil
}

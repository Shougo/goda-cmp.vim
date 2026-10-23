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
// The source option ignoreCase ignores case completely.
type Params struct {
	// CaseSensitive turns the case folding off completely.
	CaseSensitive bool `json:"caseSensitive"`
}

type filter struct{ p Params }

// New returns the filter, named "matcherhead".
func New(p Params) cmp.Filter { return filter{p} }

func (filter) Name() string { return "matcherhead" }

// WithParams sets the parameters from Vim script (filterParams).
func (f filter) WithParams(p cmp.Params) (any, error) {
	np, err := cmp.PatchParams(f.p, p)
	if err != nil {
		return nil, err
	}
	return New(np), nil
}

func (f filter) Filter(_ context.Context, in cmp.FilterInput) ([]cmp.Item, error) {
	typed := in.Complete
	fold := in.IgnoreCase || (!f.p.CaseSensitive && !strings.ContainsFunc(typed, unicode.IsUpper))
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

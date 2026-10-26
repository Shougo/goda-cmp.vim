package cmptest

import (
	"context"
	"strings"
	"unicode"

	"github.com/Shougo/goda-cmp.vim/cmp"
	"github.com/Shougo/goda.vim/sdk"
)

// WordsParams are the parameters of the source NewWords returns.
type WordsParams struct {
	// MinLength drops the shorter words (in bytes; default 1).
	MinLength int `json:"minLength"`
	// Extra are offered after the words of the buffer.
	Extra []string `json:"extra"`
}

type words struct{ p WordsParams }

// NewWords returns a source for tests, named "words": in insert mode the
// words of the buffer (runs of letters, digits and "_"), from the first line
// on, then p.Extra; never what is being typed, and each once. It filters
// nothing: that is for the matchers (see NewHead). Its parameters may be
// set from Vim script.
func NewWords(p WordsParams) cmp.Source {
	if p.MinLength <= 0 {
		p.MinLength = 1
	}
	return words{p}
}

func (words) Name() string { return "words" }

func (w words) WithParams(p cmp.Params) (any, error) {
	np, err := cmp.PatchParams(w.p, p)
	if err != nil {
		return nil, err
	}
	return NewWords(np), nil
}

func (w words) Gather(ctx context.Context, h sdk.Host, c *cmp.Context) (cmp.Gathered, error) {
	start := c.KeywordStart()
	var cands []string
	if c.Mode == "" || c.Mode == "i" {
		v, err := h.Call(ctx, "getbufline", c.Bufnr, 1, "$")
		if err != nil {
			return cmp.Gathered{}, err
		}
		l, _ := v.([]any)
		for _, x := range l {
			s, _ := x.(string)
			cands = append(cands, strings.FieldsFunc(s, func(r rune) bool { return !cmp.IsWord(r) })...)
		}
	}
	cands = append(cands, w.p.Extra...)
	seen := map[string]bool{c.Input[start:]: true}
	var items []cmp.Item
	for _, s := range cands {
		if seen[s] || len(s) < w.p.MinLength {
			continue
		}
		seen[s] = true
		items = append(items, cmp.Item{Word: s})
	}
	return cmp.Gathered{Start: start, Items: items}, nil
}

type head struct{}

// NewHead returns a matcher for tests, named "head": the items whose
// Filter() starts with what is typed, the case ignored unless something
// typed is upper case (or always with the ignoreCase option).
func NewHead() cmp.Filter { return head{} }

func (head) Name() string { return "head" }

func (head) Filter(_ context.Context, in cmp.FilterInput) ([]cmp.Item, error) {
	typed := in.Complete
	fold := in.IgnoreCase || !strings.ContainsFunc(typed, unicode.IsUpper)
	if fold {
		typed = strings.ToLower(typed)
	}
	out := make([]cmp.Item, 0, len(in.Items))
	for _, it := range in.Items {
		s := it.Filter()
		if fold {
			s = strings.ToLower(s)
		}
		if strings.HasPrefix(s, typed) {
			out = append(out, it)
		}
	}
	return out, nil
}

var (
	_ cmp.Configurable = words{}
	_ cmp.Filter       = head{}
)

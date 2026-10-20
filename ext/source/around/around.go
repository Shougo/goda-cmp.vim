// Package around is a completion source for the words of the current buffer,
// nearest lines first.
package around

import (
	"context"
	"strings"
	"unicode"

	"github.com/Shougo/goda-cmp.vim/cmp"
	"github.com/Shougo/goda.vim/sdk"
)

// Params are the settings of the source. The zero value is usable.
type Params struct {
	// MinLength: shorter words are not offered (default 2).
	MinLength int
	// MaxLines: only this many lines around the cursor are scanned
	// (default 5000).
	MaxLines int
}

type source struct{ p Params }

// New returns the source, named "around".
func New(p Params) cmp.Source {
	if p.MinLength <= 0 {
		p.MinLength = 2
	}
	if p.MaxLines <= 0 {
		p.MaxLines = 5000
	}
	return source{p}
}

func (source) Name() string { return "around" }

func (s source) Gather(ctx context.Context, h sdk.Host, c *cmp.Context) (cmp.Gathered, error) {
	start := c.KeywordStart()
	typed := c.Input[start:]

	first, last := c.Lnum-s.p.MaxLines/2, c.Lnum+s.p.MaxLines/2
	if first < 1 {
		first = 1
	}
	raw, err := h.Call(ctx, "getbufline", c.Bufnr, first, last)
	if err != nil {
		return cmp.Gathered{}, err
	}
	lines, _ := raw.([]any)
	cur := c.Lnum - first // index of the cursor line in lines
	if cur >= len(lines) {
		cur = len(lines) - 1
	}

	seen := map[string]bool{typed: true} // never offer what is already typed
	var items []cmp.Item
	// Walk outwards from the cursor: distance 0, then +1/-1, ...
	for d := range lines {
		idx := []int{cur + d}
		if d > 0 {
			idx = append(idx, cur-d)
		}
		for _, i := range idx {
			if i < 0 || i >= len(lines) {
				continue
			}
			line, _ := lines[i].(string)
			if i == cur {
				// On the cursor line only the text before the word counts.
				line = c.Input[:start]
			}
			for _, w := range words(line) {
				if len([]rune(w)) < s.p.MinLength || seen[w] {
					continue
				}
				seen[w] = true
				items = append(items, cmp.Item{Word: w})
			}
		}
		if ctx.Err() != nil {
			return cmp.Gathered{}, ctx.Err()
		}
	}
	return cmp.Gathered{Start: start, Items: items}, nil
}

func words(line string) []string {
	return strings.FieldsFunc(line, func(r rune) bool { return !(r == '_' || unicode.IsLetter(r) || unicode.IsDigit(r)) })
}

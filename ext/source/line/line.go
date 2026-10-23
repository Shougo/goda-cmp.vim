// Package line is a completion source for whole lines of the current buffer
// around the cursor, leading white space left out. It is a port of
// ddc-source-line (https://github.com/Shougo/ddc-source-line).
package line

import (
	"context"
	"strings"
	"unicode"

	"github.com/Shougo/goda-cmp.vim/cmp"
	"github.com/Shougo/goda.vim/sdk"
)

// Params are the parameters of the source.
type Params struct {
	// MaxSize is how many lines above and below the cursor are offered
	// (default 5000).
	MaxSize int `json:"maxSize"`
}

type source struct{ p Params }

// New returns the source, named "line".
func New(p Params) cmp.Source {
	if p.MaxSize <= 0 {
		p.MaxSize = 5000
	}
	return source{p}
}

func (source) Name() string { return "line" }

// WithParams sets the parameters from Vim script (sourceParams).
func (s source) WithParams(p cmp.Params) (any, error) {
	np, err := cmp.PatchParams(s.p, p)
	if err != nil {
		return nil, err
	}
	return New(np), nil
}

func (s source) Gather(ctx context.Context, h sdk.Host, c *cmp.Context) (cmp.Gathered, error) {
	// From after the indent of the text typed.
	start := len(c.Input) - len(strings.TrimLeftFunc(c.Input, unicode.IsSpace))
	v, err := h.Call(ctx, "getbufline", c.Bufnr, max(1, c.Lnum-s.p.MaxSize), c.Lnum+s.p.MaxSize)
	if err != nil {
		return cmp.Gathered{}, err
	}
	lines, _ := v.([]any)
	items := make([]cmp.Item, 0, len(lines))
	for _, l := range lines {
		line, _ := l.(string)
		items = append(items, cmp.Item{Word: strings.TrimLeftFunc(line, unicode.IsSpace)})
	}
	return cmp.Gathered{Start: start, Items: items}, nil
}

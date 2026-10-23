// Package register is a completion source for the contents of registers. It
// is a port of ddc-source-register
// (https://github.com/Milly/ddc-source-register, MIT license, Copyright Milly),
// without its handling of unprintable characters: a register of several lines
// or with control characters is offered only through ExtractWords.
package register

import (
	"context"
	"fmt"
	"strings"
	"unicode"

	"github.com/Shougo/goda-cmp.vim/cmp"
	"github.com/Shougo/goda.vim/sdk"
)

// Params are the parameters of the source.
type Params struct {
	// Registers are the names of the registers, such as "012ab#"; empty
	// means all.
	Registers string `json:"registers"`
	// ExtractWords offers the words of the registers instead of their
	// contents.
	ExtractWords bool `json:"extractWords"`
}

const allRegisters = `"0123456789abcdefghijklmnopqrstuvwxyz-.:#%/=*+`

type source struct{ p Params }

// New returns the source, named "register".
func New(p Params) cmp.Source { return source{p} }

func (source) Name() string { return "register" }

// WithParams sets the parameters from Vim script (sourceParams).
func (s source) WithParams(p cmp.Params) (any, error) {
	np, err := cmp.PatchParams(s.p, p)
	if err != nil {
		return nil, err
	}
	return New(np), nil
}

func (s source) Gather(ctx context.Context, h sdk.Host, c *cmp.Context) (cmp.Gathered, error) {
	names := s.p.Registers
	if names == "" {
		names = allRegisters
	}
	regs := []any{}
	for _, r := range names {
		if strings.ContainsRune(allRegisters, r) {
			regs = append(regs, string(r))
		}
	}
	// One call: getreginfo() of every register ({} for an empty one, or
	// "*" without a clipboard).
	v, err := h.Call(ctx, "map", regs, "getreginfo(v:val)")
	if err != nil {
		return cmp.Gathered{}, err
	}
	infos, _ := v.([]any)
	var items []cmp.Item
	seen := map[string]bool{}
	for i, x := range infos {
		info, _ := x.(map[string]any)
		contents, ok := info["regcontents"].([]any)
		if !ok || i >= len(regs) {
			continue
		}
		name := regs[i].(string)
		typ, _ := info["regtype"].(string)
		var lines []string
		for _, l := range contents {
			s, _ := l.(string)
			lines = append(lines, s)
		}
		text := strings.Join(lines, "\n")
		if s.p.ExtractWords {
			for k, w := range words(text) {
				items = append(items, cmp.Item{Word: w, Info: w, Kind: "w", Menu: fmt.Sprintf("%s[%d]", name, k)})
			}
			continue
		}
		if text == "" || strings.ContainsFunc(text, unicode.IsControl) || seen[text] {
			continue
		}
		seen[text] = true
		items = append(items, cmp.Item{Word: text, Info: text, Kind: wise(typ), Menu: name})
	}
	return cmp.Gathered{Start: c.KeywordStart(), Items: items}, nil
}

// wise is the original's kind: c(haracter), l(ine) or b(lock).
func wise(typ string) string {
	switch {
	case typ == "v":
		return "c"
	case typ == "V":
		return "l"
	case strings.HasPrefix(typ, "\x16"):
		return "b"
	}
	return ""
}

// words are the keywords of text ('iskeyword' approximated by letters,
// digits and "_").
func words(text string) []string {
	return strings.FieldsFunc(text, func(r rune) bool {
		return !(r == '_' || unicode.IsLetter(r) || unicode.IsDigit(r))
	})
}

// Package vim is a completion source for Vim script: variables, options,
// functions, mappings, features, environment variables... with the help of
// the item previewed (cmp.Previewer). It is a port of ddc-source-vim
// (https://github.com/Shougo/ddc-source-vim); its Vim script is
// autoload/goda_cmp/source/vim.vim.
package vim

import (
	"context"
	"fmt"
	"regexp"

	"github.com/Shougo/goda-cmp.vim/cmp"
	"github.com/Shougo/goda.vim/sdk"
)

type source struct{}

// New returns the source, named "vim".
func New() cmp.Source { return source{} }

func (source) Name() string { return "vim" }

// keyword is what is completed, before the cursor: a variable, an option,
// a <Plug> mapping, a key, a function or an environment variable.
var keyword = regexp.MustCompile(`(?:` +
	`[a-z]:(?:[a-zA-Z_][a-zA-Z0-9_]*)?` +
	`|&(?:[gl]:)?[a-zA-Z0-9_:]*` +
	`|<Plug>\([^)]*\)?` +
	`|<[a-zA-Z][a-zA-Z0-9_-]*>?` +
	`|[a-zA-Z_][a-zA-Z0-9_:#]*[!(]?` +
	`|\$[a-zA-Z_][a-zA-Z0-9_]*` +
	`)$`)

var comment = regexp.MustCompile(`^\s*"`)

func (source) Gather(ctx context.Context, h sdk.Host, c *cmp.Context) (cmp.Gathered, error) {
	none := cmp.Gathered{Start: len(c.Input)}
	v, err := h.Call(ctx, "goda_cmp#source#vim#get_cur_text", c.Input)
	if err != nil {
		return cmp.Gathered{}, err
	}
	if cur, _ := v.(string); comment.MatchString(cur) {
		return none, nil
	}
	loc := keyword.FindStringIndex(c.Input)
	if loc == nil {
		return none, nil
	}
	start := loc[0]
	v, err = h.Call(ctx, "goda_cmp#source#vim#gather", c.Input, c.Input[start:])
	if err != nil {
		return cmp.Gathered{}, err
	}
	l, _ := v.([]any)
	items := make([]cmp.Item, 0, len(l))
	for _, x := range l {
		m, ok := x.(map[string]any)
		if !ok {
			continue
		}
		it := cmp.Item{Word: str(m["word"]), Abbr: str(m["abbr"]), Menu: str(m["menu"]), Kind: str(m["kind"]), Info: str(m["info"])}
		// The word as the source made it, for Preview (the engine may put
		// what other sources start with in front of Word).
		it.UserData = it.Word
		if it.Word != "" {
			items = append(items, it)
		}
	}
	return cmp.Gathered{Start: start, Items: items}, nil
}

// Preview shows the help of the item, when there is any, as the original.
func (source) Preview(ctx context.Context, h sdk.Host, a cmp.DoneArgs) (cmp.Preview, error) {
	word, ok := a.Item.UserData.(string)
	if !ok {
		word = a.Item.Word
	}
	v, err := h.Call(ctx, "getcompletion", word, "help")
	if err != nil {
		return cmp.Preview{}, err
	}
	if l, _ := v.([]any); len(l) == 0 {
		return cmp.Preview{Kind: cmp.PreviewEmpty}, nil
	}
	return cmp.Preview{Kind: cmp.PreviewHelp, Tag: word}, nil
}

func str(v any) string {
	if v == nil {
		return ""
	}
	if s, ok := v.(string); ok {
		return s
	}
	return fmt.Sprint(v)
}

var _ cmp.Previewer = source{}

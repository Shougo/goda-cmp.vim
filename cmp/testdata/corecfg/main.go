// Command corecfg is the configuration of the core's end-to-end tests: the
// test UI and a source of its own, so that no extension is involved.
package main

import (
	"context"
	"os"
	"regexp"
	"strings"

	"github.com/Shougo/goda-cmp.vim/cmp"
	"github.com/Shougo/goda-cmp.vim/cmp/cmptest"
	"github.com/Shougo/goda.vim/sdk"
)

// words offers the words of the buffer (in insert mode) and those of
// $GODA_WORDS, in that order, that start with what is typed.
type words struct{}

func (words) Name() string { return "words" }

var word = regexp.MustCompile(`\w+`)

func (words) Gather(ctx context.Context, h sdk.Host, c *cmp.Context) (cmp.Gathered, error) {
	start := c.KeywordStart()
	prefix := c.Input[start:]
	var cands []string
	if c.Mode == "i" {
		v, err := h.Eval(ctx, "getline(1, '$')")
		if err != nil {
			return cmp.Gathered{}, err
		}
		l, _ := v.([]any)
		for i, x := range l {
			s, _ := x.(string)
			if i+1 == c.Lnum {
				// Not the word being typed.
				s = strings.TrimSuffix(s, prefix)
			}
			cands = append(cands, word.FindAllString(s, -1)...)
		}
	}
	cands = append(cands, strings.Fields(os.Getenv("GODA_WORDS"))...)
	seen := map[string]bool{}
	var items []cmp.Item
	for _, w := range cands {
		if seen[w] || !strings.HasPrefix(w, prefix) || w == prefix {
			continue
		}
		seen[w] = true
		items = append(items, cmp.Item{Word: w})
	}
	// Filtered here (there is no matcher): asked again for every key.
	return cmp.Gathered{Start: start, Items: items, Incomplete: true}, nil
}

func main() {
	b := cmp.New()
	b.AddUI(cmptest.NewUI("test", "i", "c", "t"))
	b.AddSource(words{})
	b.PatchGlobal(cmp.Config{
		UI:                      "test",
		Sources:                 []string{"words"},
		CmdlineSources:          map[string][]string{":": {"words"}},
		SpecialBufferCompletion: new(true),
		AutoCompleteDelay:       new(10),
	})
	b.Main()
}

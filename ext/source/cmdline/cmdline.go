// Package cmdline is a completion source for the command line: the items of
// getcompletion(input, 'cmdline'). It is a port of ddc-source-cmdline
// (https://github.com/Shougo/ddc-source-cmdline).
//
// Use it in Config.CmdlineSources, with a UI that works on the command line
// (ext/ui/pum):
//
//	b.AddSource(cmdline.New())
//	b.PatchGlobal(cmp.Config{
//		CmdlineSources: map[string][]string{":": {"cmdline"}},
//		SourceOptions:  map[string]cmp.SourceOption{"cmdline": {Mark: "cmdline"}},
//	})
//
// For fuzzy matched items, put "fuzzy" in 'wildoptions'. In insert mode (a
// Vim script buffer) it completes the command of the current line, joining
// continuation lines ("\") above it.
package cmdline

import (
	"context"
	"fmt"
	"regexp"
	"strings"

	"github.com/Shougo/goda-cmp.vim/cmp"
	"github.com/Shougo/goda.vim/sdk"
)

type source struct{}

// New returns the source, named "cmdline".
func New() cmp.Source { return source{} }

func (source) Name() string { return "cmdline" }

var (
	continuation = regexp.MustCompile(`^\s*\\`)
	helpPrefix   = regexp.MustCompile(`^help\s+`)
)

func (source) Gather(ctx context.Context, h sdk.Host, c *cmp.Context) (cmp.Gathered, error) {
	start := c.KeywordStart()
	completeStr := c.Input[start:]

	// No completion for searches, input(), the expression register...
	cmdType := c.CmdType
	if c.Mode != "c" {
		v, err := h.Call(ctx, "getcmdtype")
		if err != nil {
			return cmp.Gathered{}, err
		}
		cmdType, _ = v.(string)
	}
	switch cmdType {
	case "/", "?", ">", "=", "@":
		return cmp.Gathered{Start: start}, nil
	}

	input := c.Input
	if c.Mode != "c" {
		// Join the continuation lines above.
		for lnum := c.Lnum; strings.HasPrefix(strings.TrimSpace(input), `\`) && lnum > 1; lnum-- {
			v, err := h.Call(ctx, "getline", lnum-1)
			if err != nil {
				return cmp.Gathered{}, err
			}
			prev, _ := v.(string)
			input = prev + continuation.ReplaceAllString(input, "")
		}
	}

	var results []string
	if v, err := h.Call(ctx, "getcompletion", input, "cmdline"); err == nil {
		for _, r := range asList(v) {
			results = append(results, fmt.Sprint(r))
		}
	} // errors are ignored, as in ddc-source-cmdline

	// getcompletion() does not return the "no" prefixed options.
	if strings.HasPrefix(completeStr, "no") && strings.HasPrefix(c.Input, "set") {
		for i, w := range results {
			results[i] = "no" + w
		}
	}

	// getcompletion() returns items for ":help" that do not start with the
	// typed text.
	if strings.HasPrefix(c.Input, "help ") {
		prefix := strings.ToLower(helpPrefix.ReplaceAllString(c.Input, ""))
		kept := results[:0]
		for _, w := range results {
			if strings.HasPrefix(strings.ToLower(w), prefix) {
				kept = append(kept, w)
			}
		}
		results = kept
	}

	if len(results) == 0 {
		return cmp.Gathered{Start: start}, nil
	}

	results = trimCommonPrefix(results, input, completeStr)

	// "**/foo.txt" must match "foo.txt".
	suffix := strings.ToLower(completeStr[strings.LastIndex(completeStr, "/")+1:])
	if suffix != "" {
		kept := results[:0]
		for _, w := range results {
			var rs string
			if strings.HasSuffix(w, "/") {
				rs = w[:strings.LastIndex(w, "/")]
			} else {
				rs = w[strings.LastIndex(w, "/")+1:]
			}
			if strings.HasPrefix(strings.ToLower(rs), suffix) {
				kept = append(kept, w)
			}
		}
		results = kept
	}

	items := make([]cmp.Item, 0, len(results))
	for _, w := range results {
		if strings.HasSuffix(w, "/") {
			items = append(items, cmp.Item{Word: strings.TrimSuffix(w, "/"), Abbr: w})
		} else {
			items = append(items, cmp.Item{Word: w})
		}
	}
	return cmp.Gathered{Start: start, Items: items}, nil
}

// trimCommonPrefix is ddc-source-cmdline's adjustment of what
// getcompletion() returns to the completed word: the part of the results'
// common prefix that the input ends with, beyond the completed word, is
// removed from every result. It works in characters, as the original does.
func trimCommonPrefix(results []string, input, completeStr string) []string {
	prefix := []rune(strings.ToLower(results[0]))
	for _, w := range results {
		lw := strings.ToLower(w)
		for !strings.HasPrefix(lw, string(prefix)) {
			prefix = prefix[:len(prefix)-1]
		}
	}
	compareInput := strings.ToLower(input)
	for !strings.HasSuffix(compareInput, string(prefix)) {
		prefix = prefix[:len(prefix)-1]
	}
	if len(prefix) == 0 || string(prefix) == completeStr {
		return results
	}
	n := len(prefix) - len([]rune(completeStr)) // JS substring: never negative
	if n <= 0 {
		return results
	}
	out := make([]string, len(results))
	for i, w := range results {
		r := []rune(w)
		out[i] = string(r[min(n, len(r)):])
	}
	return out
}

func asList(v any) []any {
	switch l := v.(type) {
	case []any:
		return l
	case []string:
		out := make([]any, len(l))
		for i, s := range l {
			out[i] = s
		}
		return out
	}
	return nil
}

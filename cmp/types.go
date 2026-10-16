// Package cmp is the completion engine of goda-cmp.vim.
//
// It defines three kinds of extension, all plain Go values that the user links
// into one binary (see Builder):
//
//   - Source: produces candidates.
//   - Filter: a matcher, sorter or converter; which one it is depends on the
//     list it is placed in (Config.SourceOptions), not on the filter itself.
//   - UI: shows the candidates, either with Vim's native popup ("native") or
//     with pum.vim ("pum").
//
// Extensions live in their own modules; this package only fixes the
// interfaces. A configuration is Go code (see examples/config), so it is type
// checked and completed by the editor like any other program.
package cmp

import (
	"context"
	"regexp"
	"unicode"
	"unicode/utf8"

	"github.com/Shougo/goda.vim/sdk"
)

// Context describes where completion was requested. It is what the Vim side
// sends and what every Source and Filter receives.
type Context struct {
	// Seq identifies the request; Tick is b:changedtick at that time. The
	// Vim side drops an answer that no longer matches.
	Seq  int64 `json:"seq"`
	Tick int64 `json:"tick"`

	Bufnr    int    `json:"bufnr"`
	Lnum     int    `json:"lnum"`
	Col      int    `json:"col"`   // 1-based byte column of the cursor
	Input    string `json:"input"` // the text of the line before the cursor
	Filetype string `json:"filetype"`
	// Manual is set when the user asked for completion explicitly; sources
	// then ignore MinAutoCompleteLength.
	Manual bool `json:"manual"`
	// Mode is "i" (insert mode). "c" (command line) is reserved.
	Mode string `json:"mode"`

	// keyword is the source's keywordPattern, anchored at the end of Input.
	keyword *regexp.Regexp
}

// KeywordStart returns the byte offset in Input where the word under the
// cursor begins (len(Input) when there is none). It follows the source's
// keywordPattern option when one is set, and KeywordStart(c.Input) otherwise.
func (c *Context) KeywordStart() int {
	if c.keyword == nil {
		return KeywordStart(c.Input)
	}
	if loc := c.keyword.FindStringIndex(c.Input); loc != nil {
		return loc[0]
	}
	return len(c.Input)
}

// KeywordStart is the default notion of "the word being typed": the longest
// suffix of input made of letters, digits and underscores.
func KeywordStart(input string) int {
	i := len(input)
	for i > 0 {
		r, size := utf8.DecodeLastRuneInString(input[:i])
		if !isWord(r) {
			break
		}
		i -= size
	}
	return i
}

func isWord(r rune) bool { return r == '_' || unicode.IsLetter(r) || unicode.IsDigit(r) }

// IsWord reports whether r is a word character in the sense of KeywordStart.
func IsWord(r rune) bool { return isWord(r) }

// Item is one candidate. The JSON names are those of Vim's complete-items.
type Item struct {
	Word string `json:"word"`
	Abbr string `json:"abbr,omitempty"`
	Menu string `json:"menu,omitempty"`
	Kind string `json:"kind,omitempty"`
	Info string `json:"info,omitempty"`

	// Source is the name of the source that produced the item.
	Source string `json:"-"`
}

// Display is what the user sees in the list: Abbr when set, otherwise Word.
func (i Item) Display() string {
	if i.Abbr != "" {
		return i.Abbr
	}
	return i.Word
}

// Gathered is the answer of a Source.
type Gathered struct {
	// Start is the byte offset in Context.Input where the completed text
	// begins. Items replace Input[Start:]. Most sources use
	// Context.KeywordStart().
	Start int
	Items []Item
}

// Source produces candidates.
//
// Gather must return when ctx is done. It may call the editor through h. A
// returned error or a panic only disables this source for this request.
type Source interface {
	Name() string
	Gather(ctx context.Context, h sdk.Host, c *Context) (Gathered, error)
}

// FilterInput is what a Filter works on.
type FilterInput struct {
	Context *Context
	// Complete is the text the candidates are matched against:
	// Context.Input[start:] for the source being processed.
	Complete string
	Items    []Item
}

// Filter transforms a list of candidates: it may drop (matcher), reorder
// (sorter) or rewrite (converter) them. It must not modify in.Items in place
// when another goroutine may still see it; returning a new slice is always
// safe.
type Filter interface {
	Name() string
	Filter(ctx context.Context, in FilterInput) ([]Item, error)
}

// ShowRequest is what a UI is asked to display.
type ShowRequest struct {
	Context *Context
	// Col is the 1-based byte column where the completed text starts, the
	// first argument of Vim's complete().
	Col   int
	Items []Item
}

// UI shows candidates in the editor.
type UI interface {
	Name() string
	Show(ctx context.Context, h sdk.Host, req ShowRequest) error
	Hide(ctx context.Context, h sdk.Host) error
}

type namedSource struct {
	Source
	name string
}

func (n namedSource) Name() string { return n.name }

// NamedSource registers s under another name, so that the same source can be
// added twice with different parameters.
func NamedSource(name string, s Source) Source { return namedSource{s, name} }

type namedFilter struct {
	f    Filter
	name string
}

func (n namedFilter) Name() string { return n.name }
func (n namedFilter) Filter(ctx context.Context, in FilterInput) ([]Item, error) {
	return n.f.Filter(ctx, in)
}

// NamedFilter is NamedSource for filters.
func NamedFilter(name string, f Filter) Filter { return namedFilter{f, name} }

// Package cmp is the completion engine of goda-cmp.vim.
//
// It defines three kinds of extension, all plain Go values that the user links
// into one binary (see Builder):
//
//   - Source: produces candidates.
//   - Filter: a matcher, sorter or converter; which one it is depends on the
//     list it is placed in (Config.SourceOptions), not on the filter itself.
//   - UI: shows the candidates; ext/ui/native uses Vim's popup menu and
//     ext/ui/pum uses pum.vim. A configuration adds the ones it uses.
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

	Bufnr int    `json:"bufnr"`
	Lnum  int    `json:"lnum"`
	Col   int    `json:"col"`   // 1-based byte column of the cursor
	Input string `json:"input"` // the text of the line before the cursor
	// NextInput is the text of the line after the cursor.
	NextInput string `json:"nextinput"`
	Filetype  string `json:"filetype"`
	// Manual is set when the user asked for completion explicitly;
	// MinAutoCompleteLength is ignored then.
	Manual bool `json:"manual"`
	// Mode is "i" (insert mode) or "c" (the command line, see
	// goda_cmp#enable_cmdline()). On the command line, Input is the command
	// line before the cursor and Col the cursor's byte column in it; Lnum and
	// Bufnr are those of the current window.
	Mode string `json:"mode"`
	// CmdType is getcmdtype() on the command line (":", "/", "@", ...).
	CmdType string `json:"cmdtype"`
	// Event is the autocmd that started the request (TextChangedI,
	// TextChangedP), "" for goda_cmp#complete(). A UI's Skipper may look at
	// it.
	Event string `json:"event"`

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
	// Dup (1) keeps this item when another one has the same Word, which
	// Vim would otherwise drop.
	Dup int `json:"dup,omitempty"`

	// FilterText is what matchers compare with when it is not empty
	// (LSP filterText); otherwise Word. SortText orders the items of a
	// source for sorters that care (LSP sortText); see Item.Sort.
	FilterText string `json:"-"`
	SortText   string `json:"-"`
	// UserData belongs to the source that produced the item. It stays in
	// the plugin process and is handed back to OnCompleteDone and Resolve.
	UserData any `json:"-"`

	// Source is the name of the source that produced the item.
	Source string `json:"-"`

	// ref identifies the item in the list a UI showed; the engine sets it
	// (as Vim's user_data) just before the UI sees the items.
	Ref string `json:"user_data,omitempty"`
}

// Filter returns the text matchers should compare with.
func (i Item) Filter() string {
	if i.FilterText != "" {
		return i.FilterText
	}
	return i.Word
}

// Sort returns the text sorters should compare with.
func (i Item) Sort() string {
	if i.SortText != "" {
		return i.SortText
	}
	return i.Word
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
	// Incomplete says that the items are not all there are (LSP
	// isIncomplete): the source is asked again on the next keystroke
	// instead of its items being filtered again (see SourceOption.IsVolatile).
	Incomplete bool
}

// Source produces candidates.
//
// Gather must return when ctx is done. It may call the editor through h. A
// returned error or a panic only disables this source for this request.
type Source interface {
	Name() string
	Gather(ctx context.Context, h sdk.Host, c *Context) (Gathered, error)
}

// StreamSource is a Source that can hand over candidates as they arrive
// instead of all at once, which is what a language server needs: one server
// answers fast, another slowly, and the first should not wait for the second.
//
// Every emit replaces what this source emitted before in the same request, so
// a re-ranked or shortened list is expressed by emitting it again. GatherStream
// returns when the source has nothing more to add. emit fails once the request
// is superseded; the source should stop then. Items emitted before a timeout
// are kept.
//
// A StreamSource must also be a Source. Collect makes that a one-liner.
type StreamSource interface {
	Source
	GatherStream(ctx context.Context, h sdk.Host, c *Context, emit func(Gathered) error) error
}

// Collect implements Source.Gather on top of GatherStream: it returns the
// last emitted value.
func Collect(ctx context.Context, h sdk.Host, c *Context, s interface {
	GatherStream(context.Context, sdk.Host, *Context, func(Gathered) error) error
}) (Gathered, error) {
	var last Gathered
	err := s.GatherStream(ctx, h, c, func(g Gathered) error {
		last = g
		return ctx.Err()
	})
	return last, err
}

// CompletePositioner is implemented by sources that know where the text
// they complete starts, as ddc.vim's getCompletePosition: an input method's
// reading, which no keywordPattern describes. CompletePosition returns the
// byte offset in Context.Input, or -1 when there is nothing to complete
// (the source is not asked then). The engine uses it instead of the keyword
// for the completed lengths (minAutoCompleteLength and the others); the
// source still answers its Gathered.Start. It is called for every request,
// so it must be fast.
type CompletePositioner interface {
	CompletePosition(ctx context.Context, h sdk.Host, c *Context) int
}

// Initializer is implemented by sources that need to start something (a
// connection, an index) before they can answer. Init runs once, in the
// background of the first request that uses the source, and is bounded by
// sourceTimeout. A source whose Init failed is disabled for the rest of the
// process.
type Initializer interface {
	Init(ctx context.Context, h sdk.Host) error
}

// Closer is implemented by sources that hold something to release (a language
// server process, a connection). Close is called once when the plugin stops:
// the editor went away, or goda#stop() was called. It may run while requests
// are still in flight, and also for a source whose Init never ran. The editor
// is gone, so there is no sdk.Host. It needs goda.vim with sdk.Closer; with an
// older one it is never called.
type Closer interface {
	Close(ctx context.Context) error
}

// DoneArgs is what OnCompleteDone and Resolve receive.
type DoneArgs struct {
	// Context is the context of the request that produced Item.
	Context *Context
	// Item is the candidate as the source returned it, with UserData; the
	// converters have not touched UserData.
	Item Item
}

// CompleteDoner is implemented by sources that act when one of their
// candidates is accepted (an LSP source applies additionalTextEdits there).
// It is called after the text has been inserted. Errors are logged.
type CompleteDoner interface {
	OnCompleteDone(ctx context.Context, h sdk.Host, a DoneArgs) error
}

// Resolver is implemented by sources whose candidates can be completed
// lazily (LSP completionItem/resolve), typically to fill Info when the item
// is selected. Resolve returns the updated item.
type Resolver interface {
	Resolve(ctx context.Context, h sdk.Host, a DoneArgs) (Item, error)
}

// Preview is what a Previewer shows for an item, in ddc.vim's previewer
// format: a UI that has a preview window (pum.vim, through
// goda_cmp#get_previewer()) shows it. Kind is one of the Preview* constants.
type Preview struct {
	Kind string `json:"kind"`
	// Tag is the help tag shown for PreviewHelp.
	Tag string `json:"tag,omitempty"`
	// Contents are the lines of PreviewText and PreviewMarkdown.
	Contents []string `json:"contents,omitempty"`
	// Command is the Ex command run in the preview window for
	// PreviewCommand.
	Command string `json:"command,omitempty"`
}

// The kinds of Preview.
const (
	PreviewEmpty    = "empty" // nothing: the UI shows the item's Info
	PreviewHelp     = "help"
	PreviewText     = "text"
	PreviewMarkdown = "markdown"
	PreviewCommand  = "command"
)

// Previewer is implemented by sources that can show more about an item than
// its Info: the help of a command, the contents of a file. Preview is asked
// when the UI opens its preview window for the item; an empty Kind counts as
// PreviewEmpty.
type Previewer interface {
	Preview(ctx context.Context, h sdk.Host, a DoneArgs) (Preview, error)
}

// FilterInput is what a Filter works on.
type FilterInput struct {
	Context *Context
	// Complete is the text the candidates are matched against:
	// Context.Input[start:] for the source being processed.
	Complete string
	Items    []Item
	// Host reaches the editor, for filters that need it (a sorter that
	// ranks by the command-line history). It is nil when the filter is
	// called outside the engine, in tests for instance.
	Host sdk.Host
	// IgnoreCase is the ignoreCase option of the source: a matcher should
	// ignore case completely when it is set. It is false for postFilters.
	IgnoreCase bool
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
	// Final is false while sources are still working (see StreamSource).
	// Pending lists them. A UI may skip non-final updates, for instance
	// while the user is choosing an item.
	Final   bool
	Pending []string
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

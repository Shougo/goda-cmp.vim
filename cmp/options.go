package cmp

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"maps"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/Shougo/goda.vim/sdk"
)

// SourceOption configures one source. The key "_" in Config.SourceOptions
// holds the defaults every source inherits. The options are those of ddc.vim
// (see the ddc-source-options of its documentation), with the same names.
//
// A nil field means "not set"; a non-nil (even empty) slice replaces the
// inherited one. Fields whose zero value is a real setting are pointers:
// write new(0).
type SourceOption struct {
	// Mark is shown in the menu column of items that have none.
	Mark string `json:"mark"`
	// MinAutoCompleteLength: shorter words do not trigger this source
	// automatically (default 1).
	MinAutoCompleteLength *int `json:"minAutoCompleteLength"`
	// MaxAutoCompleteLength: longer words do not trigger this source
	// automatically (default 80), which helps in files of long tokens.
	MaxAutoCompleteLength *int `json:"maxAutoCompleteLength"`
	// MinManualCompleteLength and MaxManualCompleteLength are the same for
	// goda_cmp#complete() (default 0 and 80).
	MinManualCompleteLength *int `json:"minManualCompleteLength"`
	MaxManualCompleteLength *int `json:"maxManualCompleteLength"`
	// MinKeywordLength and MaxKeywordLength drop items whose word is
	// shorter or longer, in characters (default 0: no limit).
	MinKeywordLength *int `json:"minKeywordLength"`
	MaxKeywordLength *int `json:"maxKeywordLength"`
	// MaxItems caps the items of this source after sorting (default 100,
	// 0 = unlimited).
	MaxItems *int `json:"maxItems"`
	// KeywordPattern is a regular expression (Go's RE2 syntax: no lookaround)
	// for the text before the cursor that is completed; the match must end
	// at the cursor. Empty means letters, digits and underscores.
	KeywordPattern string `json:"keywordPattern"`
	// ForceCompletionPattern is a regular expression (RE2) for the text
	// before the cursor; the match must end at the cursor. When it
	// matches, the completed lengths are ignored, as in ddc.vim: `\.` lets
	// a language server complete members right after the dot.
	ForceCompletionPattern string `json:"forceCompletionPattern"`
	// IgnoreCase is handed to the filters (FilterInput.IgnoreCase); a
	// matcher that honors it ignores case completely (default false).
	IgnoreCase *bool `json:"ignoreCase"`
	// MatcherKey makes the matchers compare another attribute of the items
	// instead of the word: "abbr", "menu", "kind" or "info".
	MatcherKey string `json:"matcherKey"`
	// Dup decides what happens to an item whose word another item already
	// has: "ignore" drops it (default), "keep" keeps both, "force" drops
	// the earlier one.
	Dup string `json:"dup"`
	// EnabledIf is a Vim expression evaluated for every request; the source
	// is used only when it is true. Empty means always.
	EnabledIf string `json:"enabledIf"`
	// Timeout bounds the source and its filters, in milliseconds (default
	// Config.SourceTimeout).
	Timeout *int `json:"timeout"`
	// HideTimeout: when the source has not answered after this many
	// milliseconds, the popup is hidden, so that it does not show stale
	// candidates (default 200). A plain source is then left out of the
	// request, as in ddc.vim; a StreamSource goes on.
	HideTimeout *int `json:"hideTimeout"`
	// The engine keeps what a source gathered and, while the word is typed
	// on (the same line, the same start), filters it again instead of
	// asking the source. IsVolatile asks the source every time instead,
	// for sources whose answer depends on all the text typed; with
	// VolatilePattern only when it matches the text before the cursor.
	// A source that answers Gathered.Incomplete is asked again anyway, and
	// goda_cmp#complete() always asks.
	IsVolatile      *bool  `json:"isVolatile"`
	VolatilePattern string `json:"volatilePattern"`
	// CacheTimeout, in seconds, keeps what the source gathered for that
	// long, even when the word goes out of the completed lengths in the
	// meantime (default 0: not kept then).
	CacheTimeout *int `json:"cacheTimeout"`
	// ReplaceSourceInputPattern is a regular expression (RE2) whose first
	// match is removed from the text before the cursor that the source
	// sees, useful with fuzzy matchers.
	ReplaceSourceInputPattern string `json:"replaceSourceInputPattern"`
	// Preview turns the source's Previewer off when false (default true).
	Preview *bool `json:"preview"`

	Matchers   []string `json:"matchers"`
	Sorters    []string `json:"sorters"`
	Converters []string `json:"converters"`

	// DynamicFilters, when set, may replace the filters for one request,
	// after the items are gathered. Set in Go only.
	DynamicFilters func(ctx context.Context, h sdk.Host, a DynamicFiltersArgs) (Filters, error) `json:"-"`
}

// Filters is the answer of SourceOption.DynamicFilters: a nil list keeps the
// one of the options.
type Filters struct {
	Matchers   []string
	Sorters    []string
	Converters []string
}

// DynamicFiltersArgs is the input of SourceOption.DynamicFilters.
type DynamicFiltersArgs struct {
	Context *Context
	Source  string // the name of the source
	// Complete is the text the items are matched against.
	Complete string
	Items    []Item
}

// DynamicSourcesArgs is the input of Config.DynamicSources.
type DynamicSourcesArgs struct {
	Context *Context
	// Sources are the sources the configuration enables.
	Sources []string
}

// DynamicUIArgs is the input of Config.DynamicUI.
type DynamicUIArgs struct {
	Context *Context
	Items   []Item
}

// DynamicConfigArgs is the input of Config.DynamicConfig.
type DynamicConfigArgs struct {
	Context *Context
}

// Params are the parameters of an extension set from Vim script
// (sourceParams, filterParams, uiParams): a JSON object whose keys are those
// of the extension's parameters. See Configurable.
type Params map[string]any

// Config is both the effective configuration and a patch: PatchGlobal and
// PatchFiletype (and the Vim-side goda_cmp#patch_*) merge a Config into what
// is there, field by field, in the way ddc#custom#patch_global does. Unset
// fields (zero value, nil) keep the previous setting; maps are merged by key;
// slices are replaced.
//
// The JSON names are the ones ddc.vim uses, since the Vim side sends patches as
// dictionaries. Unknown keys are an error. The functions cannot come from Vim
// script; they are set in Go only.
type Config struct {
	// UI is the name of a UI added to the builder (AddUI), such as "native"
	// or "pum". It must be set: there is no default.
	UI string `json:"ui"`
	// Sources lists the enabled sources in order.
	Sources       []string                `json:"sources"`
	SourceOptions map[string]SourceOption `json:"sourceOptions"`
	// CmdlineSources lists the sources of the command line by its type
	// (getcmdtype(): ":", "/", "@", ...), as ddc.vim's cmdlineSources: on
	// the command line they replace Sources. The UI must be able to show
	// candidates there (ModesUI with "c", such as "pum"). Merged by key.
	CmdlineSources map[string][]string `json:"cmdlineSources"`
	// PostFilters run on the merged list of all sources.
	PostFilters []string `json:"postFilters"`

	// SourceParams, FilterParams and UIParams set the parameters of
	// extensions by name, over those they were built with in Go. The
	// extension must implement Configurable. Merged by name, and for one
	// name, by key.
	SourceParams map[string]Params `json:"sourceParams"`
	FilterParams map[string]Params `json:"filterParams"`
	UIParams     map[string]Params `json:"uiParams"`

	// AutoCompleteDelay is the debounce time in milliseconds before a
	// request is sent after typing (default 0). nil keeps the previous
	// value.
	AutoCompleteDelay *int `json:"autoCompleteDelay"`
	// AutoCompleteEvents are the autocmd events that complete; see
	// DefaultAutoCompleteEvents.
	AutoCompleteEvents []string `json:"autoCompleteEvents"`
	// BackspaceCompletion completes after a backspace too (default false:
	// the popup is closed instead, which flickers less).
	BackspaceCompletion *bool `json:"backspaceCompletion"`
	// HideOnEvents closes the popup before every completion (default
	// false).
	HideOnEvents *bool `json:"hideOnEvents"`
	// SpecialBufferCompletion completes in buffers whose 'buftype' is not
	// empty, such as a terminal (default false). The command line is
	// always completed.
	SpecialBufferCompletion *bool `json:"specialBufferCompletion"`
	// ContextFiletype is where the filetype of a request comes from, for
	// files with embedded languages: "none" (default) is 'filetype',
	// "context_filetype" asks context_filetype.vim, and "treesitter" the
	// tree-sitter parser of Neovim at the cursor. It decides which filetype
	// settings apply, so it can be set globally or for a buffer only (it is
	// ddc#enable()'s context_filetype).
	ContextFiletype string `json:"contextFiletype"`
	// PartialInterval is the least time in milliseconds between two
	// updates of the popup while a StreamSource is still answering
	// (default 50). The first update is not delayed.
	PartialInterval *int `json:"partialInterval"`
	// AutoComplete turns completion while typing on or off (default on).
	AutoComplete *bool `json:"autoComplete"`
	// SourceTimeout bounds every source and filter, in milliseconds
	// (default 2000); SourceOption.Timeout sets it for one source.
	SourceTimeout *int `json:"sourceTimeout"`

	// DynamicConfig, when set, returns a patch laid over the configuration
	// of one request, deciding with the help of the editor (h): other
	// sources in a comment, say. It is ddc.vim's ddc#custom#set_context_*()
	// in Go. The zero Config changes nothing.
	DynamicConfig func(ctx context.Context, h sdk.Host, a DynamicConfigArgs) (Config, error) `json:"-"`
	// DynamicSources, when set, may replace the list of sources for one
	// request, deciding with the help of the editor (h). Returning nil keeps
	// the list.
	DynamicSources func(ctx context.Context, h sdk.Host, a DynamicSourcesArgs) ([]string, error) `json:"-"`
	// DynamicUI, when set, may choose the UI for one request after the items
	// are known. Returning "" keeps Config.UI.
	DynamicUI func(ctx context.Context, h sdk.Host, a DynamicUIArgs) (string, error) `json:"-"`
}

// DefaultAutoCompleteEvents is the default of Config.AutoCompleteEvents.
// ddc.vim's has the first three only; the others fire only after
// goda_cmp#enable_cmdline() and goda_cmp#enable_terminal(), so they are in by
// default here.
var DefaultAutoCompleteEvents = []string{"InsertEnter", "TextChangedI", "TextChangedP", "CmdlineEnter", "CmdlineChanged", "TextChangedT"}

func defaultConfig() Config {
	return Config{
		SourceOptions: map[string]SourceOption{
			"_": {
				MinAutoCompleteLength:   new(1),
				MaxAutoCompleteLength:   new(80),
				MinManualCompleteLength: new(0),
				MaxManualCompleteLength: new(80),
				MinKeywordLength:        new(0),
				MaxKeywordLength:        new(0),
				MaxItems:                new(100),
				IgnoreCase:              new(false),
				Dup:                     "ignore",
				HideTimeout:             new(200),
				IsVolatile:              new(false),
				CacheTimeout:            new(0),
				Preview:                 new(true),
			},
		},
		AutoCompleteDelay:       new(0),
		AutoCompleteEvents:      slices.Clone(DefaultAutoCompleteEvents),
		BackspaceCompletion:     new(false),
		HideOnEvents:            new(false),
		SpecialBufferCompletion: new(false),
		PartialInterval:         new(50),
		AutoComplete:            new(true),
		SourceTimeout:           new(2000),
	}
}

// merge returns base patched with p. Neither argument is modified.
func merge(base, p Config) Config {
	out := base
	setStr(&out.UI, p.UI)
	if p.Sources != nil {
		out.Sources = slices.Clone(p.Sources)
	}
	if p.PostFilters != nil {
		out.PostFilters = slices.Clone(p.PostFilters)
	}
	if p.AutoCompleteEvents != nil {
		out.AutoCompleteEvents = slices.Clone(p.AutoCompleteEvents)
	}
	if base.CmdlineSources != nil || p.CmdlineSources != nil {
		out.CmdlineSources = make(map[string][]string, len(base.CmdlineSources)+len(p.CmdlineSources))
		maps.Copy(out.CmdlineSources, base.CmdlineSources)
		for k, v := range p.CmdlineSources {
			out.CmdlineSources[k] = slices.Clone(v)
		}
	}
	out.SourceParams = mergeParams(base.SourceParams, p.SourceParams)
	out.FilterParams = mergeParams(base.FilterParams, p.FilterParams)
	out.UIParams = mergeParams(base.UIParams, p.UIParams)
	setStr(&out.ContextFiletype, p.ContextFiletype)
	setPtr(&out.PartialInterval, p.PartialInterval)
	setPtr(&out.AutoCompleteDelay, p.AutoCompleteDelay)
	setPtr(&out.AutoComplete, p.AutoComplete)
	setPtr(&out.SourceTimeout, p.SourceTimeout)
	setPtr(&out.BackspaceCompletion, p.BackspaceCompletion)
	setPtr(&out.HideOnEvents, p.HideOnEvents)
	setPtr(&out.SpecialBufferCompletion, p.SpecialBufferCompletion)
	if p.DynamicConfig != nil {
		out.DynamicConfig = p.DynamicConfig
	}
	if p.DynamicSources != nil {
		out.DynamicSources = p.DynamicSources
	}
	if p.DynamicUI != nil {
		out.DynamicUI = p.DynamicUI
	}
	out.SourceOptions = make(map[string]SourceOption, len(base.SourceOptions)+len(p.SourceOptions))
	maps.Copy(out.SourceOptions, base.SourceOptions)
	for k, v := range p.SourceOptions {
		out.SourceOptions[k] = mergeOption(out.SourceOptions[k], v)
	}
	return out
}

func setPtr[T any](dst **T, p *T) {
	if p != nil {
		*dst = p
	}
}

func setStr(dst *string, p string) {
	if p != "" {
		*dst = p
	}
}

// mergeParams merges the parameters of each name by key.
func mergeParams(base, p map[string]Params) map[string]Params {
	if base == nil && p == nil {
		return nil
	}
	out := make(map[string]Params, len(base)+len(p))
	maps.Copy(out, base)
	for name, ps := range p {
		m := make(Params, len(out[name])+len(ps))
		maps.Copy(m, out[name])
		maps.Copy(m, ps)
		out[name] = m
	}
	return out
}

func mergeOption(base, p SourceOption) SourceOption {
	out := base
	setStr(&out.Mark, p.Mark)
	setPtr(&out.MinAutoCompleteLength, p.MinAutoCompleteLength)
	setPtr(&out.MaxAutoCompleteLength, p.MaxAutoCompleteLength)
	setPtr(&out.MinManualCompleteLength, p.MinManualCompleteLength)
	setPtr(&out.MaxManualCompleteLength, p.MaxManualCompleteLength)
	setPtr(&out.MinKeywordLength, p.MinKeywordLength)
	setPtr(&out.MaxKeywordLength, p.MaxKeywordLength)
	setPtr(&out.MaxItems, p.MaxItems)
	setStr(&out.KeywordPattern, p.KeywordPattern)
	setStr(&out.ForceCompletionPattern, p.ForceCompletionPattern)
	setPtr(&out.IgnoreCase, p.IgnoreCase)
	setStr(&out.MatcherKey, p.MatcherKey)
	setStr(&out.Dup, p.Dup)
	setStr(&out.EnabledIf, p.EnabledIf)
	setPtr(&out.Timeout, p.Timeout)
	setPtr(&out.HideTimeout, p.HideTimeout)
	setPtr(&out.IsVolatile, p.IsVolatile)
	setStr(&out.VolatilePattern, p.VolatilePattern)
	setPtr(&out.CacheTimeout, p.CacheTimeout)
	setStr(&out.ReplaceSourceInputPattern, p.ReplaceSourceInputPattern)
	setPtr(&out.Preview, p.Preview)
	if p.Matchers != nil {
		out.Matchers = p.Matchers
	}
	if p.Sorters != nil {
		out.Sorters = p.Sorters
	}
	if p.Converters != nil {
		out.Converters = p.Converters
	}
	if p.DynamicFilters != nil {
		out.DynamicFilters = p.DynamicFilters
	}
	return out
}

// sourceOption is the option of one source: its own entry laid over "_".
func (c Config) sourceOption(name string) SourceOption {
	return mergeOption(c.SourceOptions["_"], c.SourceOptions[name])
}

// sourceTimeout is the timeout of one source.
func (c Config) sourceTimeout(o SourceOption) time.Duration {
	if o.Timeout != nil && *o.Timeout > 0 {
		return time.Duration(*o.Timeout) * time.Millisecond
	}
	return c.timeout()
}

func (c Config) timeout() time.Duration {
	if c.SourceTimeout == nil || *c.SourceTimeout <= 0 {
		return 2 * time.Second
	}
	return time.Duration(*c.SourceTimeout) * time.Millisecond
}

func deref[T any](p *T, def T) T {
	if p == nil {
		return def
	}
	return *p
}

// keywordRegexp compiles a keywordPattern or a forceCompletionPattern so that
// it must end at the cursor.
func keywordRegexp(pattern string) (*regexp.Regexp, error) {
	re, err := regexp.Compile("(?:" + pattern + ")$")
	if err != nil {
		return nil, fmt.Errorf("pattern %q: %w", pattern, err)
	}
	return re, nil
}

// decodeConfig reads a patch sent by the editor. Unknown keys and wrongly
// typed values are errors, so that a typo is reported instead of ignored.
func decodeConfig(v any) (Config, error) {
	var c Config
	if v == nil {
		return c, nil
	}
	b, err := json.Marshal(v)
	if err != nil {
		return c, err
	}
	dec := json.NewDecoder(bytes.NewReader(b))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&c); err != nil {
		return Config{}, fmt.Errorf("config: %w", err)
	}
	return c, nil
}

// Patches are the settings made at run time from Vim script, which the editor
// sends with every request: {global, filetype: {ft: patch}, buffer}.
type Patches struct {
	Global   Config
	Filetype map[string]Config
	Buffer   Config
	// Aliases are the names made with goda_cmp#alias().
	Aliases Aliases
	// Manual is laid over everything else: the options of
	// goda_cmp#complete({options}).
	Manual Config
}

// Aliases are names given to extensions from Vim script, as ddc#custom#alias()
// does: an alias is the extension it names, with parameters of its own
// (sourceParams and so on, under the alias).
type Aliases struct {
	Source map[string]string `json:"source"`
	Filter map[string]string `json:"filter"`
	UI     map[string]string `json:"ui"`
}

func decodePatches(v any) (Patches, error) {
	var p Patches
	if v == nil {
		return p, nil
	}
	m, ok := v.(map[string]any)
	if !ok {
		return p, fmt.Errorf("patches must be a dictionary, got %T", v)
	}
	var err error
	for k, val := range m {
		switch k {
		case "global":
			p.Global, err = decodeConfig(val)
		case "buffer":
			p.Buffer, err = decodeConfig(val)
		case "aliases":
			p.Aliases, err = decodeAliases(val)
		case "manual":
			p.Manual, err = decodeConfig(val)
		case "filetype":
			fts, ok := val.(map[string]any)
			if !ok {
				return p, fmt.Errorf("patches.filetype must be a dictionary")
			}
			p.Filetype = map[string]Config{}
			for ft, c := range fts {
				if p.Filetype[ft], err = decodeConfig(c); err != nil {
					return p, fmt.Errorf("filetype %s: %w", ft, err)
				}
			}
		default:
			return p, fmt.Errorf("patches: unknown key %q", k)
		}
		if err != nil {
			return p, fmt.Errorf("%s: %w", k, err)
		}
	}
	return p, nil
}

func joinErrs(errs []string) string { return strings.Join(errs, "\n  ") }

func decodeAliases(v any) (Aliases, error) {
	var a Aliases
	if v == nil {
		return a, nil
	}
	b, err := json.Marshal(v)
	if err != nil {
		return a, err
	}
	dec := json.NewDecoder(bytes.NewReader(b))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&a); err != nil {
		return Aliases{}, fmt.Errorf("aliases: %w", err)
	}
	return a, nil
}

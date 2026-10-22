package cmp

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"maps"
	"regexp"
	"strings"
	"time"

	"github.com/Shougo/goda.vim/sdk"
)

// SourceOption configures one source. The key "_" in Config.SourceOptions
// holds the defaults every source inherits.
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
	// MaxItems caps the items of this source after sorting (default 100,
	// 0 = unlimited).
	MaxItems *int `json:"maxItems"`
	// KeywordPattern is a regular expression (Go's RE2 syntax: no lookaround)
	// for the text before the cursor that is completed; the match must end
	// at the cursor. Empty means letters, digits and underscores.
	KeywordPattern string `json:"keywordPattern"`
	// ForceCompletionPattern is a regular expression (RE2) for the text
	// before the cursor; the match must end at the cursor. When it
	// matches, MinAutoCompleteLength is ignored, as in ddc.vim: `\.` lets
	// a language server complete members right after the dot.
	ForceCompletionPattern string `json:"forceCompletionPattern"`

	Matchers   []string `json:"matchers"`
	Sorters    []string `json:"sorters"`
	Converters []string `json:"converters"`
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

// Config is both the effective configuration and a patch: PatchGlobal and
// PatchFiletype (and the Vim-side goda_cmp#patch_*) merge a Config into what
// is there, field by field, in the way ddc#custom#patch_global does. Unset
// fields (zero value, nil) keep the previous setting; maps are merged by key;
// slices are replaced.
//
// The JSON names are the ones ddc.vim uses, since the Vim side sends patches as
// dictionaries. Unknown keys are an error. The two functions cannot come from
// Vim script; they are set in Go only.
type Config struct {
	// UI is the name of a UI added to the builder (AddUI), such as "native"
	// or "pum". It must be set: there is no default.
	UI string `json:"ui"`
	// Sources lists the enabled sources in order.
	Sources       []string                `json:"sources"`
	SourceOptions map[string]SourceOption `json:"sourceOptions"`
	// PostFilters run on the merged list of all sources.
	PostFilters []string `json:"postFilters"`

	// AutoCompleteDelay is the debounce time in milliseconds before a
	// request is sent after typing (default 0). nil keeps the previous
	// value.
	AutoCompleteDelay *int `json:"autoCompleteDelay"`
	// PartialInterval is the least time in milliseconds between two
	// updates of the popup while a StreamSource is still answering
	// (default 50). The first update is not delayed.
	PartialInterval *int `json:"partialInterval"`
	// AutoComplete turns completion while typing on or off (default on).
	AutoComplete *bool `json:"autoComplete"`
	// SourceTimeout bounds every source and filter, in milliseconds
	// (default 2000).
	SourceTimeout *int `json:"sourceTimeout"`

	// DynamicSources, when set, may replace the list of sources for one
	// request, deciding with the help of the editor (h). Returning nil keeps
	// the list.
	DynamicSources func(ctx context.Context, h sdk.Host, a DynamicSourcesArgs) ([]string, error) `json:"-"`
	// DynamicUI, when set, may choose the UI for one request after the items
	// are known. Returning "" keeps Config.UI.
	DynamicUI func(ctx context.Context, h sdk.Host, a DynamicUIArgs) (string, error) `json:"-"`
}

func defaultConfig() Config {
	return Config{
		SourceOptions: map[string]SourceOption{
			"_": {
				MinAutoCompleteLength: new(1),
				MaxItems:              new(100),
			},
		},
		AutoCompleteDelay: new(0),
		PartialInterval:   new(50),
		AutoComplete:      new(true),
		SourceTimeout:     new(2000),
	}
}

// merge returns base patched with p. Neither argument is modified.
func merge(base, p Config) Config {
	out := base
	if p.UI != "" {
		out.UI = p.UI
	}
	if p.Sources != nil {
		out.Sources = append([]string{}, p.Sources...)
	}
	if p.PostFilters != nil {
		out.PostFilters = append([]string{}, p.PostFilters...)
	}
	if p.PartialInterval != nil {
		out.PartialInterval = p.PartialInterval
	}
	if p.AutoCompleteDelay != nil {
		out.AutoCompleteDelay = p.AutoCompleteDelay
	}
	if p.AutoComplete != nil {
		out.AutoComplete = p.AutoComplete
	}
	if p.SourceTimeout != nil {
		out.SourceTimeout = p.SourceTimeout
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

func mergeOption(base, p SourceOption) SourceOption {
	out := base
	if p.Mark != "" {
		out.Mark = p.Mark
	}
	if p.MinAutoCompleteLength != nil {
		out.MinAutoCompleteLength = p.MinAutoCompleteLength
	}
	if p.MaxItems != nil {
		out.MaxItems = p.MaxItems
	}
	if p.KeywordPattern != "" {
		out.KeywordPattern = p.KeywordPattern
	}
	if p.ForceCompletionPattern != "" {
		out.ForceCompletionPattern = p.ForceCompletionPattern
	}
	if p.Matchers != nil {
		out.Matchers = p.Matchers
	}
	if p.Sorters != nil {
		out.Sorters = p.Sorters
	}
	if p.Converters != nil {
		out.Converters = p.Converters
	}
	return out
}

// sourceOption is the option of one source: its own entry laid over "_".
func (c Config) sourceOption(name string) SourceOption {
	return mergeOption(c.SourceOptions["_"], c.SourceOptions[name])
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

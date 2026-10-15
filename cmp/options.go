package cmp

import "time"

// Ptr returns a pointer to v. Config uses pointers where the zero value is a
// meaningful setting: cmp.Ptr(0).
func Ptr[T any](v T) *T { return &v }

// SourceOption configures one source. The key "_" in Config.SourceOptions
// holds the defaults every source inherits.
//
// A nil field means "not set"; a non-nil (even empty) slice replaces the
// inherited one.
type SourceOption struct {
	// Mark is shown in the menu column of items that have none.
	Mark string
	// MinAutoCompleteLength: shorter words do not trigger this source
	// automatically (default 1).
	MinAutoCompleteLength *int
	// MaxItems caps the items of this source after sorting (default 100,
	// 0 = unlimited).
	MaxItems *int

	Matchers   []string
	Sorters    []string
	Converters []string
}

// Config is both the effective configuration and a patch: PatchGlobal and
// PatchFiletype merge a Config into what is there, field by field, in the
// way ddc#custom#patch_global does. Unset fields (zero value, nil) keep the
// previous setting; maps are merged by key; slices are replaced.
type Config struct {
	// UI is "native" or "pum", or the name of a UI added to the builder.
	UI string
	// Sources lists the enabled sources in order.
	Sources       []string
	SourceOptions map[string]SourceOption
	// PostFilters run on the merged list of all sources.
	PostFilters []string

	// AutoCompleteDelay is the debounce time in milliseconds before a
	// request is sent after typing (default 0). nil keeps the previous
	// value.
	AutoCompleteDelay *int
	// AutoComplete turns completion while typing on or off (default on).
	AutoComplete *bool
	// SourceTimeout bounds every source and filter, in milliseconds
	// (default 2000).
	SourceTimeout *int
}

func defaultConfig() Config {
	return Config{
		UI: "native",
		SourceOptions: map[string]SourceOption{
			"_": {
				MinAutoCompleteLength: Ptr(1),
				MaxItems:              Ptr(100),
			},
		},
		AutoCompleteDelay: Ptr(0),
		AutoComplete:      Ptr(true),
		SourceTimeout:     Ptr(2000),
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
	if p.AutoCompleteDelay != nil {
		out.AutoCompleteDelay = p.AutoCompleteDelay
	}
	if p.AutoComplete != nil {
		out.AutoComplete = p.AutoComplete
	}
	if p.SourceTimeout != nil {
		out.SourceTimeout = p.SourceTimeout
	}
	out.SourceOptions = make(map[string]SourceOption, len(base.SourceOptions)+len(p.SourceOptions))
	for k, v := range base.SourceOptions {
		out.SourceOptions[k] = v
	}
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

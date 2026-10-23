package cmp

import (
	"context"
	"fmt"
	"log"
	"regexp"
	"slices"
	"strings"

	"github.com/Shougo/goda.vim/host"
	"github.com/Shougo/goda.vim/sdk"
)

// Builder collects extensions and configuration. It is what a user's config.go
// is written against:
//
//	b := cmp.New()
//	b.AddSource(around.New(around.Params{}))
//	b.AddFilter(matcherhead.New(matcherhead.Params{}))
//	b.PatchGlobal(cmp.Config{UI: "pum", Sources: []string{"around"}})
//	b.Main()
type Builder struct {
	sources map[string]Source
	// sourceOrder is the order of AddSource; Close runs in reverse.
	sourceOrder []string
	filters     map[string]Filter
	uis         map[string]UI

	global Config
	ft     map[string]Config
	errs   []string
}

// New returns an empty Builder: sources, filters and UIs are all added by
// the configuration.
func New() *Builder {
	b := &Builder{
		sources: map[string]Source{},
		filters: map[string]Filter{},
		uis:     map[string]UI{},
		global:  defaultConfig(),
		ft:      map[string]Config{},
	}
	return b
}

func (b *Builder) fail(format string, args ...any) {
	b.errs = append(b.errs, fmt.Sprintf(format, args...))
}

// AddSource registers a source under its Name.
func (b *Builder) AddSource(s Source) *Builder {
	if _, dup := b.sources[s.Name()]; dup {
		b.fail("source %q registered twice", s.Name())
	}
	b.sources[s.Name()] = s
	b.sourceOrder = append(b.sourceOrder, s.Name())
	return b
}

// AddFilter registers a filter under its Name.
func (b *Builder) AddFilter(f Filter) *Builder {
	if _, dup := b.filters[f.Name()]; dup {
		b.fail("filter %q registered twice", f.Name())
	}
	b.filters[f.Name()] = f
	return b
}

// AddUI registers a UI under its Name.
func (b *Builder) AddUI(u UI) *Builder {
	if _, dup := b.uis[u.Name()]; dup {
		b.fail("ui %q registered twice", u.Name())
	}
	b.uis[u.Name()] = u
	return b
}

// PatchGlobal merges c into the configuration used for every filetype.
func (b *Builder) PatchGlobal(c Config) *Builder {
	b.global = merge(b.global, c)
	return b
}

// PatchFiletype merges c into the configuration of one filetype, on top of
// the global one. Call it again for the same filetype to patch further.
func (b *Builder) PatchFiletype(filetype string, c Config) *Builder {
	b.ft[filetype] = merge(b.ft[filetype], c)
	return b
}

// PatchFiletypes is PatchFiletype for several filetypes at once.
func (b *Builder) PatchFiletypes(filetypes []string, c Config) *Builder {
	for _, ft := range filetypes {
		b.PatchFiletype(ft, c)
	}
	return b
}

// resolve is the configuration for a filetype and the run-time patches: the
// Go settings and the Vim-side settings of each scope, from the widest scope
// (global) to the narrowest (buffer). A dotted filetype such as "cpp.doxygen"
// applies the patches of each part in order.
func (b *Builder) resolve(filetype string, rt Patches) Config {
	cfg := merge(b.global, rt.Global)
	for part := range strings.SplitSeq(filetype, ".") {
		if p, ok := b.ft[part]; ok {
			cfg = merge(cfg, p)
		}
		if p, ok := rt.Filetype[part]; ok {
			cfg = merge(cfg, p)
		}
	}
	return merge(merge(cfg, rt.Buffer), rt.Manual)
}

// checkConfig lists the problems of one configuration: names that were never
// added (nor aliased), parameters the extension does not take, patterns that
// do not compile and values out of range. filetype is set for the
// configuration of a filetype.
func (b *Builder) checkConfig(where string, cfg Config, al Aliases, filetype bool) []string {
	var errs []string
	add := func(format string, args ...any) {
		errs = append(errs, where+": "+fmt.Sprintf(format, args...))
	}
	known := func(reg map[string]bool, aliases map[string]string, name string) bool {
		if reg[name] {
			return true
		}
		base, ok := aliases[name]
		return ok && reg[base]
	}
	srcs, filters, uis := names(b.sources), names(b.filters), names(b.uis)
	for alias, base := range al.Source {
		if !srcs[base] {
			add("alias %q: unknown source %q", alias, base)
		}
	}
	for alias, base := range al.Filter {
		if !filters[base] {
			add("alias %q: unknown filter %q", alias, base)
		}
	}
	for alias, base := range al.UI {
		if !uis[base] {
			add("alias %q: unknown ui %q", alias, base)
		}
	}
	if cfg.UI != "" && !known(uis, al.UI, cfg.UI) {
		add("unknown ui %q", cfg.UI)
	}
	for _, s := range cfg.Sources {
		if !known(srcs, al.Source, s) {
			add("unknown source %q", s)
		}
	}
	for typ, list := range cfg.CmdlineSources {
		for _, s := range list {
			if !known(srcs, al.Source, s) {
				add("unknown source %q in cmdlineSources[%q]", s, typ)
			}
		}
	}
	for _, f := range cfg.PostFilters {
		if !known(filters, al.Filter, f) {
			add("unknown filter %q in postFilters", f)
		}
	}
	// Parameters: the extension must take them (made on a throwaway set).
	var tmp instances
	for name, p := range cfg.SourceParams {
		if _, _, err := lookup(&tmp, "source", b.sources, al.Source, name, p); err != nil {
			add("sourceParams: %v", err)
		}
	}
	for name, p := range cfg.FilterParams {
		if _, _, err := lookup(&tmp, "filter", b.filters, al.Filter, name, p); err != nil {
			add("filterParams: %v", err)
		}
	}
	for name, p := range cfg.UIParams {
		if _, _, err := lookup(&tmp, "ui", b.uis, al.UI, name, p); err != nil {
			add("uiParams: %v", err)
		}
	}
	switch cfg.ContextFiletype {
	case "", "none", "context_filetype", "treesitter":
	default:
		add("contextFiletype must be \"none\", \"context_filetype\" or \"treesitter\", not %q", cfg.ContextFiletype)
	}
	if filetype && cfg.ContextFiletype != "" {
		add("contextFiletype decides the filetype, so it cannot be set for one: set it globally or for a buffer")
	}
	for name, o := range cfg.SourceOptions {
		if name != "_" && !known(srcs, al.Source, name) {
			add("sourceOptions for unknown source %q", name)
		}
		for _, list := range [][]string{o.Matchers, o.Sorters, o.Converters} {
			for _, f := range list {
				if !known(filters, al.Filter, f) {
					add("source %q uses unknown filter %q", name, f)
				}
			}
		}
		for _, kp := range [][2]string{{"keywordPattern", o.KeywordPattern}, {"forceCompletionPattern", o.ForceCompletionPattern}, {"volatilePattern", o.VolatilePattern}} {
			if kp[1] == "" {
				continue
			}
			if _, err := keywordRegexp(kp[1]); err != nil {
				add("source %q: %s: %v", name, kp[0], err)
			}
		}
		if o.ReplaceSourceInputPattern != "" {
			if _, err := regexp.Compile(o.ReplaceSourceInputPattern); err != nil {
				add("source %q: replaceSourceInputPattern: %v", name, err)
			}
		}
		switch o.Dup {
		case "", "ignore", "keep", "force":
		default:
			add("source %q: dup must be \"ignore\", \"keep\" or \"force\", not %q", name, o.Dup)
		}
		switch o.MatcherKey {
		case "", "word", "abbr", "menu", "kind", "info":
		default:
			add("source %q: matcherKey must be \"abbr\", \"menu\", \"kind\" or \"info\", not %q", name, o.MatcherKey)
		}
	}
	return errs
}

func names[T any](m map[string]T) map[string]bool {
	out := make(map[string]bool, len(m))
	for k := range m {
		out[k] = true
	}
	return out
}

// checkCmdlineUI reports command-line sources set with a UI that cannot show
// them (cfg is a resolved configuration).
func (b *Builder) checkCmdlineUI(where string, cfg Config) []string {
	if !hasCmdlineSources(cfg) {
		return nil
	}
	u, ok := b.uis[cfg.UI]
	if !ok {
		return nil // reported as an unknown ui
	}
	if !slices.Contains(uiModes(u), "c") {
		return []string{fmt.Sprintf("%s: cmdlineSources are set, but ui %q cannot show candidates on the command line (use one that can, such as \"pum\")", where, cfg.UI)}
	}
	return nil
}

func hasCmdlineSources(cfg Config) bool {
	for _, names := range cfg.CmdlineSources {
		if len(names) > 0 {
			return true
		}
	}
	return false
}

// Validate reports every problem of the configuration made in Go.
func (b *Builder) Validate() error {
	errs := append([]string{}, b.errs...)
	errs = append(errs, b.checkEvents()...)
	errs = append(errs, b.checkConfig("global", b.global, Aliases{}, false)...)
	if b.global.UI == "" {
		errs = append(errs, `global: no ui is set: add one (b.AddUI(native.New()), see ext/ui) and set Config.UI to its name`)
	}
	errs = append(errs, b.checkCmdlineUI("global", b.global)...)
	for ft, c := range b.ft {
		errs = append(errs, b.checkCmdlineUI("filetype "+ft, merge(b.global, c))...)
	}
	for ft, c := range b.ft {
		errs = append(errs, b.checkConfig("filetype "+ft, c, Aliases{}, true)...)
	}
	if len(errs) > 0 {
		return fmt.Errorf("goda-cmp configuration:\n  %s", joinErrs(errs))
	}
	return nil
}

// Plugin validates the configuration and returns the plugin to hand to
// host.Main (or to combine with other plugins).
func (b *Builder) Plugin() (sdk.Plugin, error) {
	if err := b.Validate(); err != nil {
		return nil, err
	}
	return newEngine(b), nil
}

// Main runs the plugin as the whole program: the last line of a config.go
// main function.
//
// A configuration error does not end the program: it still starts, so that
// goda_cmp#enable() can show the error in the editor (cmp.status), and every
// other request fails with it. It is also written to the plugin log.
func (b *Builder) Main() {
	host.Main(b.mainPlugin())
}

// mainPlugin is the plugin Main serves: the engine, or with a configuration
// error a stand-in that reports it.
func (b *Builder) mainPlugin() sdk.Plugin {
	p, err := b.Plugin()
	if err != nil {
		log.Print(err)
		return brokenPlugin{err: err, methods: newEngine(b).Handlers()}
	}
	return p
}

// brokenPlugin answers status with the configuration error and fails every
// other method of the engine with it.
type brokenPlugin struct {
	err     error
	methods map[string]sdk.Handler
}

func (brokenPlugin) Name() string { return PluginName }

func (p brokenPlugin) Handlers() map[string]sdk.Handler {
	hs := make(map[string]sdk.Handler, len(p.methods))
	for name := range p.methods {
		hs[name] = func(context.Context, sdk.Host, []any) (any, error) { return nil, p.err }
	}
	hs["status"] = func(context.Context, sdk.Host, []any) (any, error) { return p.err.Error(), nil }
	return hs
}

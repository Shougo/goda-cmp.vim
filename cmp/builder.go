package cmp

import (
	"context"
	"fmt"
	"log"
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

// New returns a Builder that already knows the "native" and "pum" UIs.
func New() *Builder {
	b := &Builder{
		sources: map[string]Source{},
		filters: map[string]Filter{},
		uis:     map[string]UI{},
		global:  defaultConfig(),
		ft:      map[string]Config{},
	}
	b.AddUI(NativeUI())
	b.AddUI(PumUI())
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
	return merge(cfg, rt.Buffer)
}

// checkConfig lists the problems of one configuration: names that were never
// added and keyword patterns that do not compile.
func (b *Builder) checkConfig(where string, cfg Config) []string {
	var errs []string
	if cfg.UI != "" {
		if _, ok := b.uis[cfg.UI]; !ok {
			errs = append(errs, fmt.Sprintf("%s: unknown ui %q", where, cfg.UI))
		}
	}
	for _, s := range cfg.Sources {
		if _, ok := b.sources[s]; !ok {
			errs = append(errs, fmt.Sprintf("%s: unknown source %q", where, s))
		}
	}
	for _, f := range cfg.PostFilters {
		if _, ok := b.filters[f]; !ok {
			errs = append(errs, fmt.Sprintf("%s: unknown filter %q in postFilters", where, f))
		}
	}
	for name, o := range cfg.SourceOptions {
		if name != "_" {
			if _, ok := b.sources[name]; !ok {
				errs = append(errs, fmt.Sprintf("%s: sourceOptions for unknown source %q", where, name))
			}
		}
		for _, list := range [][]string{o.Matchers, o.Sorters, o.Converters} {
			for _, f := range list {
				if _, ok := b.filters[f]; !ok {
					errs = append(errs, fmt.Sprintf("%s: source %q uses unknown filter %q", where, name, f))
				}
			}
		}
		for _, kp := range [][2]string{{"keywordPattern", o.KeywordPattern}, {"forceCompletionPattern", o.ForceCompletionPattern}} {
			if kp[1] == "" {
				continue
			}
			if _, err := keywordRegexp(kp[1]); err != nil {
				errs = append(errs, fmt.Sprintf("%s: source %q: %s: %v", where, name, kp[0], err))
			}
		}
	}
	return errs
}

// Validate reports every problem of the configuration made in Go.
func (b *Builder) Validate() error {
	errs := append([]string{}, b.errs...)
	errs = append(errs, b.checkEvents()...)
	errs = append(errs, b.checkConfig("global", b.global)...)
	for ft, c := range b.ft {
		errs = append(errs, b.checkConfig("filetype "+ft, c)...)
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

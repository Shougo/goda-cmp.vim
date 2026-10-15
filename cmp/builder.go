package cmp

import (
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
	filters map[string]Filter
	uis     map[string]UI

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

// resolve is the configuration for a filetype. A dotted filetype such as
// "cpp.doxygen" applies the patches of each part in order.
func (b *Builder) resolve(filetype string) Config {
	cfg := b.global
	for _, part := range strings.Split(filetype, ".") {
		if p, ok := b.ft[part]; ok {
			cfg = merge(cfg, p)
		}
	}
	return cfg
}

// Validate reports every name the configuration uses that was never added.
func (b *Builder) Validate() error {
	errs := append([]string{}, b.errs...)
	check := func(where string, cfg Config) {
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
		}
	}
	check("global", b.global)
	for ft, c := range b.ft {
		check("filetype "+ft, c)
	}
	if len(errs) > 0 {
		return fmt.Errorf("goda-cmp configuration:\n  %s", strings.Join(errs, "\n  "))
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
// main function. A configuration error ends the program with a message in the
// plugin log (echo goda#log()).
func (b *Builder) Main() {
	p, err := b.Plugin()
	if err != nil {
		log.Fatal(err)
	}
	host.Main(p)
}

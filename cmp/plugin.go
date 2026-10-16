package cmp

import (
	"context"
	"fmt"
	"log"
	"regexp"
	"sort"
	"sync"

	"github.com/Shougo/goda.vim/sdk"
)

// PluginName is the name the Vim side addresses: goda#request('cmp', ...).
const PluginName = "cmp"

type engine struct {
	b *Builder

	mu       sync.Mutex
	latest   int64
	cancel   context.CancelFunc
	keywords map[string]*keywordEntry
}

type keywordEntry struct {
	re  *regexp.Regexp
	err error
}

func newEngine(b *Builder) *engine { return &engine{b: b, keywords: map[string]*keywordEntry{}} }

func (e *engine) Name() string { return PluginName }

func (e *engine) Handlers() map[string]sdk.Handler {
	return map[string]sdk.Handler{
		// complete(context): run the pipeline and show the result. Meant to
		// be sent as a notification; newer requests cancel older ones. The
		// context carries the run-time patches under "patches".
		"complete": e.complete,
		// hide(): close the popup.
		"hide": e.hide,
		// options(filetype, patches) -> {ui, autoComplete, delay, sources}:
		// what the Vim side needs to know before it asks.
		"options": e.options,
		// validate(config): checks a patch the Vim side is about to store.
		"validate": e.validate,
		// sources() -> sorted names, for diagnostics.
		"sources": func(context.Context, sdk.Host, []any) (any, error) {
			names := make([]string, 0, len(e.b.sources))
			for n := range e.b.sources {
				names = append(names, n)
			}
			sort.Strings(names)
			return names, nil
		},
	}
}

func (e *engine) complete(ctx context.Context, h sdk.Host, params []any) (any, error) {
	if len(params) == 0 {
		return nil, fmt.Errorf("cmp.complete needs a context dictionary")
	}
	raw, ok := params[0].(map[string]any)
	if !ok {
		return nil, fmt.Errorf("cmp.complete needs a context dictionary, got %T", params[0])
	}
	var c Context
	plain := make(map[string]any, len(raw))
	for k, v := range raw {
		if k != "patches" {
			plain[k] = v
		}
	}
	if err := sdk.Decode(plain, &c); err != nil {
		return nil, err
	}
	rt, err := decodePatches(raw["patches"])
	if err != nil {
		return nil, err
	}
	cfg := e.b.resolve(c.Filetype, rt)
	if !c.Manual && !deref(cfg.AutoComplete, true) {
		return nil, nil
	}

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	e.mu.Lock()
	if c.Seq < e.latest {
		e.mu.Unlock()
		return nil, nil
	}
	e.latest = c.Seq
	if e.cancel != nil {
		e.cancel()
	}
	e.cancel = cancel
	e.mu.Unlock()

	if cfg.DynamicSources != nil {
		names, err := sdk.Guard(ctx, func() ([]string, error) {
			return cfg.DynamicSources(ctx, h, DynamicSourcesArgs{Context: &c, Sources: cfg.Sources})
		})
		if err != nil {
			log.Printf("cmp: dynamicSources: %v", err)
		} else if names != nil {
			cfg.Sources = names
		}
	}
	for _, s := range cfg.Sources {
		if _, ok := e.b.sources[s]; !ok {
			return nil, fmt.Errorf("unknown source %q", s)
		}
	}
	if len(cfg.Sources) == 0 {
		return nil, nil
	}
	ui, err := e.ui(cfg.UI)
	if err != nil {
		return nil, err
	}

	items, start, err := e.run(ctx, h, &c, cfg)
	if err != nil {
		return nil, err
	}
	if ctx.Err() != nil {
		return nil, nil // superseded
	}
	if len(items) > 0 && cfg.DynamicUI != nil {
		name, err := sdk.Guard(ctx, func() (string, error) {
			return cfg.DynamicUI(ctx, h, DynamicUIArgs{Context: &c, Items: items})
		})
		if err != nil {
			log.Printf("cmp: dynamicUi: %v", err)
		} else if name != "" {
			if ui, err = e.ui(name); err != nil {
				return nil, err
			}
		}
	}
	if len(items) == 0 {
		return nil, ui.Hide(ctx, h)
	}
	if err := ui.Show(ctx, h, ShowRequest{Context: &c, Col: start + 1, Items: items}); err != nil {
		log.Printf("cmp: ui %s: %v", ui.Name(), err)
		return nil, err
	}
	return len(items), nil
}

func (e *engine) ui(name string) (UI, error) {
	u, ok := e.b.uis[name]
	if !ok {
		return nil, fmt.Errorf("unknown ui %q", name)
	}
	return u, nil
}

func (e *engine) hide(ctx context.Context, h sdk.Host, params []any) (any, error) {
	e.mu.Lock()
	if e.cancel != nil {
		e.cancel()
		e.cancel = nil
	}
	e.mu.Unlock()
	ft := ""
	if len(params) > 0 {
		ft, _ = params[0].(string)
	}
	var rt Patches
	if len(params) > 1 {
		var err error
		if rt, err = decodePatches(params[1]); err != nil {
			return nil, err
		}
	}
	ui, err := e.ui(e.b.resolve(ft, rt).UI)
	if err != nil {
		return nil, err
	}
	return nil, ui.Hide(ctx, h)
}

func (e *engine) options(_ context.Context, _ sdk.Host, params []any) (any, error) {
	ft := ""
	if len(params) > 0 {
		ft, _ = params[0].(string)
	}
	var rt Patches
	if len(params) > 1 {
		var err error
		if rt, err = decodePatches(params[1]); err != nil {
			return nil, err
		}
	}
	cfg := e.b.resolve(ft, rt)
	return map[string]any{
		"ui":           cfg.UI,
		"autoComplete": deref(cfg.AutoComplete, true),
		"delay":        deref(cfg.AutoCompleteDelay, 0),
		"sources":      cfg.Sources,
	}, nil
}

func (e *engine) validate(_ context.Context, _ sdk.Host, params []any) (any, error) {
	if len(params) == 0 {
		return nil, fmt.Errorf("cmp.validate needs a config dictionary")
	}
	cfg, err := decodeConfig(params[0])
	if err != nil {
		return nil, err
	}
	if errs := e.b.checkConfig("config", cfg); len(errs) > 0 {
		return nil, fmt.Errorf("%s", joinErrs(errs))
	}
	return nil, nil
}

// keyword returns the compiled keywordPattern, compiling each pattern once.
func (e *engine) keyword(pattern string) (*regexp.Regexp, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if k, ok := e.keywords[pattern]; ok {
		return k.re, k.err
	}
	re, err := keywordRegexp(pattern)
	e.keywords[pattern] = &keywordEntry{re, err}
	return re, err
}

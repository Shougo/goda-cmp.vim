package cmp

import (
	"context"
	"log"
	"sort"
	"sync"

	"github.com/Shougo/goda.vim/sdk"
)

// PluginName is the name the Vim side addresses: goda#request('cmp', ...).
const PluginName = "cmp"

type engine struct {
	b *Builder

	mu     sync.Mutex
	latest int64
	cancel context.CancelFunc
}

func newEngine(b *Builder) *engine { return &engine{b: b} }

func (e *engine) Name() string { return PluginName }

func (e *engine) Handlers() map[string]sdk.Handler {
	return map[string]sdk.Handler{
		// complete(context): run the pipeline and show the result. Meant to
		// be sent as a notification; newer requests cancel older ones.
		"complete": e.complete,
		// hide(): close the popup.
		"hide": e.hide,
		// options(filetype) -> {ui, autoComplete, delay}: what the Vim side
		// needs to know before it asks.
		"options": e.options,
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
	var c Context
	if len(params) == 0 {
		return nil, errNoContext
	}
	if err := sdk.Decode(params[0], &c); err != nil {
		return nil, err
	}
	cfg := e.b.resolve(c.Filetype)
	if len(cfg.Sources) == 0 || (!c.Manual && !deref(cfg.AutoComplete, true)) {
		return nil, nil
	}
	ui := e.b.uis[cfg.UI]

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

	items, start, err := e.run(ctx, h, &c, cfg)
	if err != nil {
		return nil, err
	}
	if ctx.Err() != nil {
		return nil, nil // superseded
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
	return nil, e.b.uis[e.b.resolve(ft).UI].Hide(ctx, h)
}

func (e *engine) options(_ context.Context, _ sdk.Host, params []any) (any, error) {
	ft := ""
	if len(params) > 0 {
		ft, _ = params[0].(string)
	}
	cfg := e.b.resolve(ft)
	return map[string]any{
		"ui":           cfg.UI,
		"autoComplete": deref(cfg.AutoComplete, true),
		"delay":        deref(cfg.AutoCompleteDelay, 0),
		"sources":      cfg.Sources,
	}, nil
}

var errNoContext = errorString("cmp.complete needs a context dictionary")

type errorString string

func (e errorString) Error() string { return string(e) }

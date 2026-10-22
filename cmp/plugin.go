package cmp

import (
	"context"
	"fmt"
	"log"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

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

	inits     map[string]*initState
	ev        *events
	nextShow  int64
	shows     map[int64]*shown
	showOrder []int64
}

// initState tracks the Init of one source.
type initState struct {
	done chan struct{}
	err  error
}

// shown is a list a UI displayed, kept so that an item the user picks can be
// found again (Item.Ref is "<show id>:<index>").
type shown struct {
	c       *Context
	items   []Item
	timeout time.Duration
	ui      UI // the UI that showed the list
}

// How many shown lists are kept. Streaming makes several per request.
const keepShown = 16

type keywordEntry struct {
	re  *regexp.Regexp
	err error
}

func newEngine(b *Builder) *engine {
	e := &engine{
		b:        b,
		keywords: map[string]*keywordEntry{},
		inits:    map[string]*initState{},
		shows:    map[int64]*shown{},
	}
	e.ev = newEvents(e)
	return e
}

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
		// done(ref): the user accepted the item the UI showed with this
		// user_data; calls the source's OnCompleteDone. Meant as a
		// notification.
		"done": e.done,
		// resolve(ref) -> item: asks the source to complete the item
		// (Info, ...). Returns the item in complete-items form.
		"resolve": e.resolve,
		// select(ref): the user moved to this item; resolves it and tells
		// the Vim side through goda_cmp#_resolved. Meant as a notification.
		"select": e.selectItem,
		// events() -> [{event, filetypes}]: the autocmd events the sources
		// want (see EventHandler), for the Vim side to listen to.
		"events": e.eventsHandler,
		// event(ev): one autocmd event of a buffer, numbered by the Vim
		// side. Meant as a notification.
		"event": e.event,
		// status() -> "" or the configuration error. A binary built from
		// an invalid configuration still starts and answers this, so that
		// the Vim side can show the error (see Builder.Main).
		"status": func(context.Context, sdk.Host, []any) (any, error) { return "", nil },
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
	// A change the UI made itself (selecting an item inserts it) is not
	// typing: the UI says so (Skipper), as in ddc.vim.
	if !c.Manual {
		if u, err := e.ui(cfg.UI); err == nil {
			if sk, ok := u.(Skipper); ok {
				sctx, cancel := context.WithTimeout(ctx, cfg.timeout())
				skip, err := sdk.Guard(sctx, func() (bool, error) { return sk.SkipCompletion(sctx, h, &c) })
				cancel()
				if err != nil {
					log.Printf("cmp: ui %s: skipCompletion: %v", u.Name(), err)
				} else if skip {
					return nil, nil
				}
			}
		}
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

	shownItems := 0
	err = e.run(ctx, h, &c, cfg, func(items []Item, start int, final bool, pending []string) error {
		u := ui
		if len(items) > 0 && cfg.DynamicUI != nil {
			name, err := sdk.Guard(ctx, func() (string, error) {
				return cfg.DynamicUI(ctx, h, DynamicUIArgs{Context: &c, Items: items})
			})
			if err != nil {
				log.Printf("cmp: dynamicUi: %v", err)
			} else if name != "" {
				if u, err = e.ui(name); err != nil {
					return err
				}
			}
		}
		if len(items) == 0 {
			shownItems = 0
			return u.Hide(ctx, h)
		}
		shownItems = len(items)
		items = e.remember(&c, cfg.timeout(), u, items)
		if err := u.Show(ctx, h, ShowRequest{Context: &c, Col: start + 1, Items: items, Final: final, Pending: pending}); err != nil {
			log.Printf("cmp: ui %s: %v", u.Name(), err)
			return err
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	if ctx.Err() != nil {
		return nil, nil // superseded
	}
	return shownItems, nil
}

// remember keeps the list for done/resolve and returns a copy whose items
// carry their reference.
func (e *engine) remember(c *Context, timeout time.Duration, ui UI, items []Item) []Item {
	out := make([]Item, len(items))
	copy(out, items)
	e.mu.Lock()
	defer e.mu.Unlock()
	e.nextShow++
	id := e.nextShow
	for i := range out {
		out[i].Ref = strconv.FormatInt(id, 10) + ":" + strconv.Itoa(i)
	}
	e.shows[id] = &shown{c: c, items: out, timeout: timeout, ui: ui}
	e.showOrder = append(e.showOrder, id)
	for len(e.showOrder) > keepShown {
		delete(e.shows, e.showOrder[0])
		e.showOrder = e.showOrder[1:]
	}
	return out
}

func (e *engine) lookup(params []any) (Item, *shown, error) {
	if len(params) == 0 {
		return Item{}, nil, fmt.Errorf("a reference is needed")
	}
	ref, _ := params[0].(string)
	idStr, idxStr, ok := strings.Cut(ref, ":")
	id, err1 := strconv.ParseInt(idStr, 10, 64)
	idx, err2 := strconv.Atoi(idxStr)
	if !ok || err1 != nil || err2 != nil {
		return Item{}, nil, fmt.Errorf("bad reference %q", ref)
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	sh := e.shows[id]
	if sh == nil || idx < 0 || idx >= len(sh.items) {
		return Item{}, nil, fmt.Errorf("reference %q is no longer known", ref)
	}
	return sh.items[idx], sh, nil
}

func (e *engine) done(ctx context.Context, h sdk.Host, params []any) (any, error) {
	it, sh, err := e.lookup(params)
	if err != nil {
		return nil, err
	}
	d, ok := e.b.sources[it.Source].(CompleteDoner)
	if !ok {
		return nil, nil
	}
	ctx, cancel := context.WithTimeout(ctx, sh.timeout)
	defer cancel()
	_, err = sdk.Guard(ctx, func() (struct{}, error) {
		return struct{}{}, d.OnCompleteDone(ctx, h, DoneArgs{Context: sh.c, Item: it})
	})
	if err != nil {
		log.Printf("cmp: source %s: onCompleteDone: %v", it.Source, err)
	}
	return nil, err
}

func (e *engine) resolveItem(ctx context.Context, h sdk.Host, params []any) (*Item, *shown, error) {
	it, sh, err := e.lookup(params)
	if err != nil {
		return nil, nil, err
	}
	r, ok := e.b.sources[it.Source].(Resolver)
	if !ok {
		return nil, nil, nil
	}
	ctx, cancel := context.WithTimeout(ctx, sh.timeout)
	defer cancel()
	out, err := sdk.Guard(ctx, func() (Item, error) {
		return r.Resolve(ctx, h, DoneArgs{Context: sh.c, Item: it})
	})
	if err != nil {
		log.Printf("cmp: source %s: resolve: %v", it.Source, err)
		return nil, nil, err
	}
	out.Ref = it.Ref
	out.Source = it.Source
	// Keep the resolved item for a later done.
	e.mu.Lock()
	if id, idx, ok := splitRef(it.Ref); ok {
		if s := e.shows[id]; s != nil && idx < len(s.items) {
			s.items[idx] = out
		}
	}
	e.mu.Unlock()
	return &out, sh, nil
}

func (e *engine) resolve(ctx context.Context, h sdk.Host, params []any) (any, error) {
	it, _, err := e.resolveItem(ctx, h, params)
	if err != nil || it == nil {
		return nil, err
	}
	return sdk.Generic(*it)
}

// selectItem resolves the item the user moved to and hands it to the UI
// that showed it (InfoUpdater), which knows how to show it.
func (e *engine) selectItem(ctx context.Context, h sdk.Host, params []any) (any, error) {
	it, sh, err := e.resolveItem(ctx, h, params)
	if err != nil || it == nil {
		return nil, err
	}
	u, ok := sh.ui.(InfoUpdater)
	if !ok {
		return nil, nil
	}
	ctx, cancel := context.WithTimeout(ctx, sh.timeout)
	defer cancel()
	_, err = sdk.Guard(ctx, func() (struct{}, error) { return struct{}{}, u.UpdateInfo(ctx, h, *it) })
	if err != nil {
		log.Printf("cmp: ui %s: updateInfo: %v", sh.ui.Name(), err)
	}
	return nil, err
}

func splitRef(ref string) (int64, int, bool) {
	a, b, ok := strings.Cut(ref, ":")
	id, err1 := strconv.ParseInt(a, 10, 64)
	idx, err2 := strconv.Atoi(b)
	return id, idx, ok && err1 == nil && err2 == nil
}

// ensureInit runs the source's Init once. The first request that needs it
// starts it; others wait for it. Init is not tied to a request, so a request
// that is superseded does not abort it.
func (e *engine) ensureInit(ctx context.Context, h sdk.Host, name string, src Source, timeout time.Duration) error {
	in, ok := src.(Initializer)
	if !ok {
		return nil
	}
	e.mu.Lock()
	st := e.inits[name]
	if st == nil {
		st = &initState{done: make(chan struct{})}
		e.inits[name] = st
		go func() {
			ictx, cancel := context.WithTimeout(context.Background(), timeout)
			defer cancel()
			_, err := sdk.Guard(ictx, func() (struct{}, error) { return struct{}{}, in.Init(ictx, h) })
			st.err = err
			close(st.done)
		}()
	}
	e.mu.Unlock()
	select {
	case <-st.done:
		return st.err
	case <-ctx.Done():
		return ctx.Err()
	}
}

// Close closes the sources that implement Closer, in the reverse order of
// their registration. goda.vim's host calls it (sdk.Closer) when the plugin
// stops.
func (e *engine) Close(ctx context.Context) error {
	e.ev.close()
	var errs []string
	for i := len(e.b.sourceOrder) - 1; i >= 0; i-- {
		name := e.b.sourceOrder[i]
		c, ok := e.b.sources[name].(Closer)
		if !ok {
			continue
		}
		if _, err := sdk.Guard(ctx, func() (struct{}, error) { return struct{}{}, c.Close(ctx) }); err != nil {
			errs = append(errs, fmt.Sprintf("source %s: %v", name, err))
		}
	}
	if len(errs) > 0 {
		return fmt.Errorf("%s", strings.Join(errs, "; "))
	}
	return nil
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

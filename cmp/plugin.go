package cmp

import (
	"context"
	"fmt"
	"log"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode"

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
	inst      instances // extensions made by WithParams

	// What the sources gathered, by source (see prevResult).
	cacheMu sync.Mutex
	prev    map[string]*prevResult

	// visible is what the engine knows of the popup: shown by the last
	// request and not hidden since. last is the list shown last, for
	// goda_cmp#map#complete(). manual is set while a list from
	// goda_cmp#complete() is open: typing on narrows it as a manual
	// completion with the same sources and UI (as in ddc.vim).
	visible bool
	last    *lastList
	manual  *manualState
	// shownUI is the UI that showed the last list. When DynamicUI picks
	// another one, it is hidden first, as in ddc.vim.
	shownUI UI
}

type lastList struct {
	c     *Context
	col   int
	items []Item
	cfg   Config
	al    Aliases
}

type manualState struct {
	bufnr   int
	mode    string
	input   string
	sources []string
	ui      string
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
	ui      UI                // the UI that showed the list
	srcs    map[string]Source // the sources of the items, by name
	preview map[string]bool   // the preview option of each
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
		prev:     map[string]*prevResult{},
	}
	e.ev = newEvents(e)
	return e
}

// source, filter and uiFor find an extension by the name a configuration
// uses: an added one or an alias, with the parameters the configuration
// gives that name.
func (e *engine) source(cfg Config, al Aliases, name string) (Source, string, error) {
	return lookup(&e.inst, "source", e.b.sources, al.Source, name, cfg.SourceParams[name])
}

func (e *engine) filter(cfg Config, al Aliases, name string) (Filter, error) {
	f, _, err := lookup(&e.inst, "filter", e.b.filters, al.Filter, name, cfg.FilterParams[name])
	return f, err
}

func (e *engine) uiFor(cfg Config, al Aliases, name string) (UI, error) {
	u, _, err := lookup(&e.inst, "ui", e.b.uis, al.UI, name, cfg.UIParams[name])
	return u, err
}

func (e *engine) Name() string { return PluginName }

func (e *engine) Handlers() map[string]sdk.Handler {
	return map[string]sdk.Handler{
		// complete(context): run the pipeline and show the result. Meant to
		// be sent as a notification; newer requests cancel older ones. The
		// context carries the run-time patches under "patches", and may
		// carry "options" (a patch for this request only, from
		// goda_cmp#complete()), "hide" (close the popup first) and
		// "update" ({name, items}: goda_cmp#update_items()).
		"complete": e.complete,
		// hide(filetype, patches): close the popup.
		"hide": e.hide,
		// options(filetype, patches) -> {ui, autoComplete, delay, ...}:
		// what the Vim side needs to know before it asks.
		"options": e.options,
		// current(filetype, patches) -> the configuration in effect.
		"current": e.current,
		// visible(filetype, patches) -> whether the popup is open.
		"visible": e.visibleHandler,
		// show(ui, filetype, patches): show the last list again with the
		// UI ui (goda_cmp#map#complete()).
		"show": e.showLast,
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
		// preview(ref) -> previewer: what the source's Previewer shows for
		// the item ({"kind": "empty"} when it has none), in ddc.vim's
		// previewer format. See goda_cmp#get_previewer().
		"preview": e.preview,
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
		// validate(config, aliases): checks a patch the Vim side is about
		// to store.
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

// The keys of the context dictionary that are not Context fields.
var requestKeys = []string{"patches", "options", "hide", "update"}

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
		if !slices.Contains(requestKeys, k) {
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
	if o, ok := raw["options"]; ok && o != nil {
		if rt.Manual, err = decodeConfig(o); err != nil {
			return nil, fmt.Errorf("options: %w", err)
		}
	}
	if u, ok := raw["update"].(map[string]any); ok {
		name, _ := u["name"].(string)
		var items []Item
		if err := sdk.Decode(u["items"], &items); err != nil {
			return nil, fmt.Errorf("update: %w", err)
		}
		for i := range items {
			items[i].Ref = ""
		}
		if !e.updateItems(name, items) {
			return nil, nil // nothing to update: no completion either
		}
	}
	al := rt.Aliases
	cfg := e.b.resolve(c.Filetype, rt)
	if cfg.DynamicConfig != nil {
		patch, err := sdk.Guard(ctx, func() (Config, error) {
			return cfg.DynamicConfig(ctx, h, DynamicConfigArgs{Context: &c})
		})
		if err != nil {
			log.Printf("cmp: dynamicConfig: %v", err)
		} else {
			cfg = merge(cfg, patch)
		}
	}

	// The command line has its own sources.
	if c.Mode == "c" {
		cfg.Sources = cfg.CmdlineSources[c.CmdType]
	}
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
	// Typing on in a list of goda_cmp#complete() narrows it as a manual
	// completion with the same sources and UI.
	if !c.Manual && c.Event != "Update" {
		if m := e.continueManual(&c, cfg); m != nil {
			cfg.Sources, cfg.UI = m.sources, m.ui
			c.Manual = true
		}
	}
	if !c.Manual && c.Event != "Update" {
		if !deref(cfg.AutoComplete, true) {
			return nil, nil
		}
		if c.Event != "" && !slices.Contains(cfg.AutoCompleteEvents, c.Event) {
			return nil, nil
		}
	}

	ui, err := e.uiFor(cfg, al, cfg.UI)
	if err != nil {
		return nil, err
	}
	// A change the UI made itself (selecting an item inserts it) is not
	// typing: the UI says so (Skipper), as in ddc.vim.
	if !c.Manual {
		if sk, ok := ui.(Skipper); ok {
			sctx, cancel := context.WithTimeout(ctx, cfg.timeout())
			skip, err := sdk.Guard(sctx, func() (bool, error) { return sk.SkipCompletion(sctx, h, &c) })
			cancel()
			if err != nil {
				log.Printf("cmp: ui %s: skipCompletion: %v", ui.Name(), err)
			} else if skip {
				return nil, nil
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

	// Nothing to complete (a command-line type without sources, input()
	// for instance): nothing to show either, whatever the UI.
	if len(cfg.Sources) == 0 {
		return nil, nil
	}
	// The UI must be able to show candidates in the mode (ModesUI). No
	// mode is insert mode (an older Vim side).
	mode := c.Mode
	if mode == "" {
		mode = "i"
	}
	if !slices.Contains(uiModes(ui), mode) {
		return nil, fmt.Errorf("ui %q cannot show candidates %s", cfg.UI, modeName(mode))
	}
	for _, s := range cfg.Sources {
		if _, _, err := e.source(cfg, al, s); err != nil {
			return nil, err
		}
	}

	hideUI := func(u UI) {
		if err := u.Hide(ctx, h); err != nil {
			log.Printf("cmp: ui %s: hide: %v", u.Name(), err)
		}
		e.setVisible(false)
	}
	if b, _ := raw["hide"].(bool); b {
		hideUI(ui)
	}

	// What done, resolve and preview need of the sources of a list.
	srcs := map[string]Source{}
	previews := map[string]bool{}
	for _, name := range cfg.Sources {
		if s, _, err := e.source(cfg, al, name); err == nil {
			srcs[name] = s
			previews[name] = deref(cfg.sourceOption(name).Preview, true)
		}
	}

	shownItems := 0
	err = e.run(ctx, h, &c, cfg, al, func() { hideUI(ui) }, func(items []Item, start int, final bool, pending []string) error {
		u := ui
		if len(items) > 0 && cfg.DynamicUI != nil {
			name, err := sdk.Guard(ctx, func() (string, error) {
				return cfg.DynamicUI(ctx, h, DynamicUIArgs{Context: &c, Items: items})
			})
			if err != nil {
				log.Printf("cmp: dynamicUi: %v", err)
			} else if name != "" {
				if u, err = e.uiFor(cfg, al, name); err != nil {
					return err
				}
			}
		}
		if len(items) == 0 {
			shownItems = 0
			e.setLast(nil)
			e.notifyItems(ctx, h, &c, 0, nil, nil)
			hideUI(u)
			e.hideShownUI(ctx, h, u)
			return nil
		}
		shownItems = len(items)
		e.mu.Lock()
		prev := e.shownUI
		e.shownUI = u
		e.mu.Unlock()
		if prev != nil && prev.Name() != u.Name() {
			if err := prev.Hide(ctx, h); err != nil {
				log.Printf("cmp: ui %s: hide: %v", prev.Name(), err)
			}
		}
		items = e.remember(&c, cfg.timeout(), u, items, srcs, previews)
		e.setLast(&lastList{c: &c, col: start + 1, items: items, cfg: cfg, al: al})
		e.notifyItems(ctx, h, &c, start+1, items, cfg.Sources)
		if err := u.Show(ctx, h, ShowRequest{Context: &c, Col: start + 1, Items: items, Final: final, Pending: pending}); err != nil {
			log.Printf("cmp: ui %s: %v", u.Name(), err)
			return err
		}
		e.setVisible(true)
		return nil
	})
	if err != nil {
		return nil, err
	}
	if ctx.Err() != nil {
		return nil, nil // superseded
	}
	e.mu.Lock()
	if c.Manual && shownItems > 0 {
		e.manual = &manualState{bufnr: c.Bufnr, mode: c.Mode, input: c.Input, sources: cfg.Sources, ui: cfg.UI}
	} else if c.Event != "Update" {
		e.manual = nil
	}
	e.mu.Unlock()
	return shownItems, nil
}

// continueManual returns the manual completion that c continues, if any:
// the list of goda_cmp#complete() is open, and c only adds to the word that
// was completed (ddc.vim's rule).
func (e *engine) continueManual(c *Context, cfg Config) *manualState {
	e.mu.Lock()
	defer e.mu.Unlock()
	m := e.manual
	if m == nil || !e.visible || m.bufnr != c.Bufnr || m.mode != c.Mode {
		return nil
	}
	if c.Event != "" && !slices.Contains(cfg.AutoCompleteEvents, c.Event) {
		return nil
	}
	if !strings.HasPrefix(c.Input, m.input) || trimWord(c.Input) != trimWord(m.input) {
		return nil
	}
	return m
}

// trimWord removes the trailing non-blank characters.
func trimWord(s string) string {
	return strings.TrimRightFunc(s, func(r rune) bool { return !unicode.IsSpace(r) })
}

func (e *engine) setVisible(v bool) {
	e.mu.Lock()
	e.visible = v
	if !v {
		e.manual = nil
	}
	e.mu.Unlock()
}

func (e *engine) setLast(l *lastList) {
	e.mu.Lock()
	e.last = l
	e.mu.Unlock()
}

// notifyItems tells the core Vim script what is shown, for the
// goda_cmp#map#*() functions (its own script: no extension is called).
func (e *engine) notifyItems(ctx context.Context, h sdk.Host, c *Context, col int, items []Item, sources []string) {
	list := []any{}
	for _, it := range items {
		if m, err := sdk.Generic(it); err == nil {
			list = append(list, m)
		}
	}
	srcs := []any{}
	for _, s := range sources {
		srcs = append(srcs, s)
	}
	if err := h.Notify(ctx, "goda_cmp#_items", map[string]any{
		"seq": c.Seq, "col": col, "items": list, "sources": srcs,
	}); err != nil && ctx.Err() == nil {
		log.Printf("cmp: goda_cmp#_items: %v", err)
	}
}

// remember keeps the list for done/resolve and returns a copy whose items
// carry their reference.
func (e *engine) remember(c *Context, timeout time.Duration, ui UI, items []Item, srcs map[string]Source, previews map[string]bool) []Item {
	out := make([]Item, len(items))
	copy(out, items)
	e.mu.Lock()
	defer e.mu.Unlock()
	e.nextShow++
	id := e.nextShow
	for i := range out {
		out[i].Ref = strconv.FormatInt(id, 10) + ":" + strconv.Itoa(i)
	}
	e.shows[id] = &shown{c: c, items: out, timeout: timeout, ui: ui, srcs: srcs, preview: previews}
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

// sourceOf is the source of an item of a shown list.
func (sh *shown) sourceOf(it Item) Source {
	if s, ok := sh.srcs[it.Source]; ok {
		return s
	}
	return nil
}

func (e *engine) done(ctx context.Context, h sdk.Host, params []any) (any, error) {
	it, sh, err := e.lookup(params)
	if err != nil {
		return nil, err
	}
	// The popup is closed by accepting.
	e.setVisible(false)
	d, ok := sh.sourceOf(it).(CompleteDoner)
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
	r, ok := sh.sourceOf(it).(Resolver)
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

func (e *engine) preview(ctx context.Context, h sdk.Host, params []any) (any, error) {
	empty := map[string]any{"kind": PreviewEmpty}
	it, sh, err := e.lookup(params)
	if err != nil {
		return nil, err
	}
	if on, ok := sh.preview[it.Source]; ok && !on {
		return empty, nil
	}
	p, ok := sh.sourceOf(it).(Previewer)
	if !ok {
		return empty, nil
	}
	ctx, cancel := context.WithTimeout(ctx, sh.timeout)
	defer cancel()
	pv, err := sdk.Guard(ctx, func() (Preview, error) {
		return p.Preview(ctx, h, DoneArgs{Context: sh.c, Item: it})
	})
	if err != nil {
		log.Printf("cmp: source %s: preview: %v", it.Source, err)
		return nil, err
	}
	if pv.Kind == "" {
		pv.Kind = PreviewEmpty
	}
	return sdk.Generic(pv)
}

func splitRef(ref string) (int64, int, bool) {
	a, b, ok := strings.Cut(ref, ":")
	id, err1 := strconv.ParseInt(a, 10, 64)
	idx, err2 := strconv.Atoi(b)
	return id, idx, ok && err1 == nil && err2 == nil
}

// ensureInit runs the source's Init once per key (see lookup). The first
// request that needs it starts it; others wait for it. Init is not tied to a
// request, so a request that is superseded does not abort it.
func (e *engine) ensureInit(ctx context.Context, h sdk.Host, key string, src Source, timeout time.Duration) error {
	in, ok := src.(Initializer)
	if !ok {
		return nil
	}
	e.mu.Lock()
	st := e.inits[key]
	if st == nil {
		st = &initState{done: make(chan struct{})}
		e.inits[key] = st
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

// Close closes the sources that implement Closer: those made by WithParams
// first, then the added ones in the reverse order of their registration.
// goda.vim's host calls it (sdk.Closer) when the plugin stops.
func (e *engine) Close(ctx context.Context) error {
	e.ev.close()
	var errs []string
	closeOne := func(what string, v any) {
		c, ok := v.(Closer)
		if !ok {
			return
		}
		if _, err := sdk.Guard(ctx, func() (struct{}, error) { return struct{}{}, c.Close(ctx) }); err != nil {
			errs = append(errs, fmt.Sprintf("%s: %v", what, err))
		}
	}
	made := e.inst.all()
	for i := len(made) - 1; i >= 0; i-- {
		if s, ok := made[i].(Source); ok {
			closeOne("source "+s.Name(), s)
		}
	}
	for i := len(e.b.sourceOrder) - 1; i >= 0; i-- {
		name := e.b.sourceOrder[i]
		closeOne("source "+name, e.b.sources[name])
	}
	if len(errs) > 0 {
		return fmt.Errorf("%s", strings.Join(errs, "; "))
	}
	return nil
}

// scope reads the (filetype, patches) arguments that several handlers take,
// from position i on, and resolves the configuration.
func (e *engine) scope(params []any, i int) (Config, Aliases, error) {
	ft := ""
	if len(params) > i {
		ft, _ = params[i].(string)
	}
	var rt Patches
	if len(params) > i+1 {
		var err error
		if rt, err = decodePatches(params[i+1]); err != nil {
			return Config{}, Aliases{}, err
		}
	}
	return e.b.resolve(ft, rt), rt.Aliases, nil
}

func (e *engine) hide(ctx context.Context, h sdk.Host, params []any) (any, error) {
	e.mu.Lock()
	if e.cancel != nil {
		e.cancel()
		e.cancel = nil
	}
	e.mu.Unlock()
	e.setVisible(false)
	e.setLast(nil)
	cfg, al, err := e.scope(params, 0)
	if err != nil {
		return nil, err
	}
	ui, err := e.uiFor(cfg, al, cfg.UI)
	if err != nil {
		return nil, err
	}
	e.hideShownUI(ctx, h, ui)
	return nil, ui.Hide(ctx, h)
}

// hideShownUI hides the UI that showed the last list when it is not ui (a
// UI DynamicUI chose), and forgets it.
func (e *engine) hideShownUI(ctx context.Context, h sdk.Host, ui UI) {
	e.mu.Lock()
	prev := e.shownUI
	e.shownUI = nil
	e.mu.Unlock()
	if prev != nil && prev.Name() != ui.Name() {
		if err := prev.Hide(ctx, h); err != nil {
			log.Printf("cmp: ui %s: hide: %v", prev.Name(), err)
		}
	}
}

func (e *engine) options(_ context.Context, _ sdk.Host, params []any) (any, error) {
	cfg, _, err := e.scope(params, 0)
	if err != nil {
		return nil, err
	}
	events := []any{}
	for _, ev := range cfg.AutoCompleteEvents {
		events = append(events, ev)
	}
	return map[string]any{
		"ui":                      cfg.UI,
		"autoComplete":            deref(cfg.AutoComplete, true),
		"delay":                   deref(cfg.AutoCompleteDelay, 0),
		"sources":                 cfg.Sources,
		"autoCompleteEvents":      events,
		"backspaceCompletion":     deref(cfg.BackspaceCompletion, false),
		"hideOnEvents":            deref(cfg.HideOnEvents, false),
		"specialBufferCompletion": deref(cfg.SpecialBufferCompletion, false),
		"contextFiletype":         cmpOr(cfg.ContextFiletype, "none"),
	}, nil
}

func (e *engine) current(_ context.Context, _ sdk.Host, params []any) (any, error) {
	cfg, _, err := e.scope(params, 0)
	if err != nil {
		return nil, err
	}
	return sdk.Generic(cfg)
}

func (e *engine) visibleHandler(ctx context.Context, h sdk.Host, params []any) (any, error) {
	cfg, al, err := e.scope(params, 0)
	if err != nil {
		return nil, err
	}
	if ui, err := e.uiFor(cfg, al, cfg.UI); err == nil {
		if v, ok := ui.(Visibler); ok {
			return sdk.Guard(ctx, func() (bool, error) { return v.Visible(ctx, h) })
		}
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.visible, nil
}

// showLast shows the last list again with another UI (ddc#map#complete()).
func (e *engine) showLast(ctx context.Context, h sdk.Host, params []any) (any, error) {
	if len(params) == 0 {
		return nil, fmt.Errorf("cmp.show needs a ui name")
	}
	name, _ := params[0].(string)
	e.mu.Lock()
	l := e.last
	e.mu.Unlock()
	if l == nil || len(l.items) == 0 {
		return nil, nil
	}
	ui, err := e.uiFor(l.cfg, l.al, name)
	if err != nil {
		return nil, err
	}
	if err := ui.Show(ctx, h, ShowRequest{Context: l.c, Col: l.col, Items: l.items, Final: true}); err != nil {
		return nil, err
	}
	e.mu.Lock()
	for _, sh := range e.shows {
		if len(sh.items) > 0 && len(l.items) > 0 && sh.items[0].Ref == l.items[0].Ref {
			sh.ui = ui
		}
	}
	e.mu.Unlock()
	e.setVisible(true)
	return nil, nil
}

func (e *engine) validate(_ context.Context, _ sdk.Host, params []any) (any, error) {
	if len(params) == 0 {
		return nil, fmt.Errorf("cmp.validate needs a config dictionary")
	}
	cfg, err := decodeConfig(params[0])
	if err != nil {
		return nil, err
	}
	var al Aliases
	if len(params) > 1 {
		if al, err = decodeAliases(params[1]); err != nil {
			return nil, err
		}
	}
	// The third argument is the scope of the patch: "global", "filetype"
	// or "buffer".
	filetype := false
	if len(params) > 2 {
		scope, _ := params[2].(string)
		filetype = scope == "filetype"
	}
	if errs := e.b.checkConfig("config", cfg, al, filetype); len(errs) > 0 {
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

// pattern returns a compiled regular expression (not anchored), compiling
// each pattern once.
func (e *engine) pattern(pattern string) (*regexp.Regexp, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	key := "\x00" + pattern
	if k, ok := e.keywords[key]; ok {
		return k.re, k.err
	}
	re, err := regexp.Compile(pattern)
	e.keywords[key] = &keywordEntry{re, err}
	return re, err
}

func cmpOr(s, def string) string {
	if s == "" {
		return def
	}
	return s
}

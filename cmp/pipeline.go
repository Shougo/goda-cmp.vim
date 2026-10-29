package cmp

import (
	"context"
	"fmt"
	"log"
	"regexp"
	"slices"
	"strconv"
	"sync"
	"sync/atomic"
	"time"
	"unicode/utf8"

	"github.com/Shougo/goda.vim/sdk"
)

// slot is what one source contributed so far.
type slot struct {
	name  string
	start int
	items []Item
	done  bool // the source returned (or failed); later emits are ignored
}

// showFunc receives the merged result. final is false while stream sources
// are still working; pending names them.
type showFunc func(items []Item, start int, final bool, pending []string) error

type runner struct {
	e       *engine
	h       sdk.Host
	c       *Context
	cfg     Config
	al      Aliases
	timeout time.Duration

	// The sources of the request, resolved (aliases, params), and the key
	// of each (see lookup).
	srcs map[string]Source
	keys map[string]string

	// hide closes the popup; hideStale calls it at most once, and only when
	// this request has shown nothing yet (hideTimeout).
	hide     func()
	hideOnce sync.Once
	shown    atomic.Bool

	mu    sync.Mutex
	slots []slot
	kick  chan struct{}
}

// run gathers, filters and merges, calling show as results arrive. Without a
// StreamSource among the enabled sources show is called once, when every
// source has answered, exactly like a plain gather. With one, it is called
// whenever something new is there, at most every PartialInterval.
func (e *engine) run(ctx context.Context, h sdk.Host, c *Context, cfg Config, al Aliases, hide func(), show showFunc) error {
	r := &runner{
		e: e, h: h, c: c, cfg: cfg, al: al, timeout: cfg.timeout(),
		srcs:  map[string]Source{},
		keys:  map[string]string{},
		hide:  hide,
		slots: make([]slot, len(cfg.Sources)),
		kick:  make(chan struct{}, 1),
	}
	partial := false
	for i, name := range cfg.Sources {
		r.slots[i].name = name
		src, key, err := e.source(cfg, al, name)
		if err != nil {
			log.Printf("cmp: %v", err)
			continue
		}
		r.srcs[name], r.keys[name] = src, key
		if _, ok := src.(StreamSource); ok {
			partial = true
		}
	}

	var wg sync.WaitGroup
	for i := range cfg.Sources {
		wg.Go(func() {
			r.gatherOne(ctx, i)
		})
	}
	allDone := make(chan struct{})
	go func() { wg.Wait(); close(allDone) }()

	interval := time.Duration(deref(cfg.PartialInterval, 50)) * time.Millisecond
	for {
		if partial {
			select {
			case <-r.kick:
			case <-allDone:
			case <-ctx.Done():
				return nil
			}
		} else {
			select {
			case <-allDone:
			case <-ctx.Done():
				return nil
			}
		}
		final := false
		select {
		case <-allDone:
			final = true
		default:
		}
		if ctx.Err() != nil {
			return nil // superseded
		}
		items, start, pending := r.merge()
		if len(items) > 0 || final {
			if len(items) > 0 {
				r.shown.Store(true)
			}
			if err := show(items, start, final, pending); err != nil {
				return err
			}
		}
		if final {
			return nil
		}
		select {
		case <-time.After(interval):
		case <-allDone:
		case <-ctx.Done():
			return nil
		}
	}
}

// hideStale closes the popup of an earlier request when a source is slow
// (hideTimeout), unless this request already shows something.
func (r *runner) hideStale() {
	if r.hide == nil || r.shown.Load() {
		return
	}
	r.hideOnce.Do(r.hide)
}

// merge combines the slots. Sources may start at different columns;
// everything is moved to the leftmost one by prepending the text in between
// to the words. Items with an empty word are dropped, and duplicated words
// are handled as the "dup" option of their source says.
func (r *runner) merge() ([]Item, int, []string) {
	r.mu.Lock()
	slots := make([]slot, len(r.slots))
	copy(slots, r.slots)
	r.mu.Unlock()

	var pending []string
	minStart := -1
	for _, s := range slots {
		if !s.done {
			pending = append(pending, s.name)
		}
		if len(s.items) > 0 && (minStart < 0 || s.start < minStart) {
			minStart = s.start
		}
	}
	if minStart < 0 {
		return nil, 0, pending
	}
	var merged []Item
	for _, s := range slots {
		if len(s.items) == 0 {
			continue // its start means nothing (0 when it never answered)
		}
		prefix := r.c.Input[minStart:s.start]
		for _, it := range s.items {
			if prefix != "" {
				it.Word = prefix + it.Word
			}
			merged = append(merged, it)
		}
	}
	if len(r.cfg.PostFilters) > 0 {
		var err error
		merged, err = r.chain(context.Background(), r.c.Input[minStart:], merged, r.cfg.PostFilters, false, r.timeout)
		if err != nil {
			log.Printf("cmp: postFilters: %v", err)
		}
	}
	return r.dedupe(merged), minStart, pending
}

// dedupe applies the "dup" option of each item's source, as ddc.vim does.
func (r *runner) dedupe(items []Item) []Item {
	dups := map[string]string{}
	out := make([]Item, 0, len(items))
	seen := map[string]bool{}
	for _, it := range items {
		if it.Word == "" {
			continue
		}
		dup, ok := dups[it.Source]
		if !ok {
			dup = r.cfg.sourceOption(it.Source).Dup
			dups[it.Source] = dup
		}
		if seen[it.Word] {
			switch dup {
			case "force":
				out = slices.DeleteFunc(out, func(o Item) bool { return o.Word == it.Word })
			case "keep":
			default: // "ignore"
				continue
			}
		}
		if dup == "keep" {
			it.Dup = 1
		}
		seen[it.Word] = true
		out = append(out, it)
	}
	return out
}

func (r *runner) store(i int, start int, items []Item, done bool) {
	r.mu.Lock()
	s := &r.slots[i]
	if s.done {
		r.mu.Unlock()
		return
	}
	s.start, s.items, s.done = start, items, done
	r.mu.Unlock()
	select {
	case r.kick <- struct{}{}:
	default:
	}
}

func (r *runner) finish(i int) { // mark done, keeping what was emitted
	r.mu.Lock()
	r.slots[i].done = true
	r.mu.Unlock()
	select {
	case r.kick <- struct{}{}:
	default:
	}
}

// truthy is Vim's notion of true for the result of an expression.
func truthy(v any) bool {
	switch x := v.(type) {
	case bool:
		return x
	case int:
		return x != 0
	case int64:
		return x != 0
	case uint64:
		return x != 0
	case int8:
		return x != 0
	case float64:
		return x != 0
	case string:
		n, _ := strconv.Atoi(x)
		return n != 0
	}
	return false
}

func (r *runner) gatherOne(ctx context.Context, i int) {
	e, c, cfg := r.e, r.c, r.cfg
	name := r.slots[i].name
	defer r.finish(i)
	src := r.srcs[name]
	if src == nil {
		return // unknown, already logged
	}
	opt := cfg.sourceOption(name)
	timeout := cfg.sourceTimeout(opt)

	if opt.EnabledIf != "" {
		v, err := r.h.Eval(ctx, opt.EnabledIf)
		if err != nil {
			if ctx.Err() == nil {
				log.Printf("cmp: source %s: enabledIf: %v", name, err)
			}
			return
		}
		if !truthy(v) {
			return
		}
	}

	// Each source sees the context with its own keyword pattern.
	sc := *c
	if opt.KeywordPattern != "" {
		re, err := e.keyword(opt.KeywordPattern)
		if err != nil {
			log.Printf("cmp: source %s: %v", name, err)
			return
		}
		sc.keyword = re
	}
	pattern := func(what, p string) *regexp.Regexp {
		if p == "" {
			return nil
		}
		re, err := e.pattern(p)
		if err != nil {
			log.Printf("cmp: source %s: %s: %v", name, what, err)
		}
		return re
	}

	// The completed lengths, unless forceCompletionPattern matches (ddc.vim's
	// rule). The word before the cursor decides whether the source is asked
	// at all; what the source answers is checked again below, against its
	// own start.
	force := false
	if opt.ForceCompletionPattern != "" {
		re, err := e.keyword(opt.ForceCompletionPattern)
		if err != nil {
			log.Printf("cmp: source %s: %v", name, err)
			return
		}
		force = re.MatchString(c.Input)
	}
	minLen, maxLen := deref(opt.MinAutoCompleteLength, 1), deref(opt.MaxAutoCompleteLength, 80)
	if c.Manual {
		minLen, maxLen = deref(opt.MinManualCompleteLength, 0), deref(opt.MaxManualCompleteLength, 80)
	}
	lengthOK := func(complete string) bool {
		n := utf8.RuneCountInString(complete)
		return force || (n >= minLen && (maxLen <= 0 || n <= maxLen))
	}

	// Where the completed text starts: the keyword, or what the source
	// says (CompletePositioner); -1 there is nothing to complete.
	kw, positioned := sc.KeywordStart(), true
	if cp, ok := src.(CompletePositioner); ok {
		pos, err := sdk.Guard(ctx, func() (int, error) { return cp.CompletePosition(ctx, r.h, &sc), nil })
		if err != nil || pos < 0 || pos > len(c.Input) {
			if err != nil && ctx.Err() == nil {
				log.Printf("cmp: source %s: completePosition: %v", name, err)
			}
			positioned = false
		} else {
			kw = pos
		}
	}
	cacheKey := r.keys[name] + "\x00" + name
	prev := e.cached(cacheKey, c, kw, deref(opt.CacheTimeout, 0))
	valid := positioned && lengthOK(c.Input[kw:])
	retry := prev != nil && prev.incomplete && valid
	if !valid && !retry {
		if deref(opt.CacheTimeout, 0) <= 0 && prev != nil {
			e.uncache(cacheKey)
		}
		return
	}
	volatile := deref(opt.IsVolatile, false)
	if volatile && opt.VolatilePattern != "" {
		re, err := e.keyword(opt.VolatilePattern)
		if err != nil {
			log.Printf("cmp: source %s: volatilePattern: %v", name, err)
		}
		volatile = re != nil && re.MatchString(c.Input)
	}
	gather := prev == nil || retry || c.Manual || (volatile && c.Event != "Update")

	sctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	// process turns what the source returned into this source's slot.
	process := func(g Gathered) ([]Item, bool) {
		if g.Start < 0 || g.Start > len(c.Input) {
			log.Printf("cmp: source %s: start %d is outside the input", name, g.Start)
			return nil, false
		}
		complete := c.Input[g.Start:]
		if !lengthOK(complete) {
			return nil, true
		}
		items, err := r.filterItems(sctx, name, opt, complete, g.Items, timeout)
		if err != nil {
			log.Printf("cmp: source %s: %v", name, err)
			return nil, false
		}
		return items, true
	}

	if !gather {
		items, ok := process(Gathered{Start: prev.start, Items: prev.items})
		if ok {
			r.store(i, prev.start, items, true)
		}
		return
	}

	if err := e.ensureInit(sctx, r.h, r.keys[name], src, timeout); err != nil {
		if ctx.Err() == nil {
			log.Printf("cmp: source %s: init: %v", name, err)
		}
		return
	}

	// replaceSourceInputPattern: the source sees the input without the first
	// match; its start is mapped back to the real input.
	at, cut := 0, 0
	if re := pattern("replaceSourceInputPattern", opt.ReplaceSourceInputPattern); re != nil {
		if loc := re.FindStringIndex(sc.Input); loc != nil && loc[1] > loc[0] {
			at, cut = loc[0], loc[1]-loc[0]
			sc.Input = sc.Input[:loc[0]] + sc.Input[loc[1]:]
		}
	}
	mapStart := func(g Gathered) Gathered {
		if cut > 0 && g.Start > at {
			g.Start += cut
		}
		return g
	}
	remember := func(g Gathered) {
		e.cache(cacheKey, &prevResult{
			bufnr: c.Bufnr, lnum: c.Lnum, mode: c.Mode + c.CmdType, kwStart: kw,
			start: g.Start, prefix: c.Input[:min(g.Start, len(c.Input))],
			items: slices.Clone(g.Items), incomplete: g.Incomplete, at: time.Now(),
		})
	}
	hideAfter := time.Duration(deref(opt.HideTimeout, 200)) * time.Millisecond

	if ss, ok := src.(StreamSource); ok {
		var timer *time.Timer
		if hideAfter > 0 {
			timer = time.AfterFunc(hideAfter, r.hideStale)
			defer timer.Stop()
		}
		var last Gathered
		got := false
		_, err := sdk.Guard(sctx, func() (struct{}, error) {
			return struct{}{}, ss.GatherStream(sctx, r.h, &sc, func(g Gathered) error {
				if err := sctx.Err(); err != nil {
					return err
				}
				if timer != nil {
					timer.Stop()
				}
				g = mapStart(g)
				last, got = g, true
				if items, ok := process(g); ok {
					r.store(i, g.Start, items, false)
				}
				return nil
			})
		})
		if err != nil {
			if ctx.Err() == nil {
				log.Printf("cmp: source %s: %v", name, err)
			}
			return // what was emitted stays
		}
		if got && ctx.Err() == nil {
			remember(last)
		}
		return
	}

	type result struct {
		g   Gathered
		err error
	}
	ch := make(chan result, 1)
	go func() {
		g, err := sdk.Guard(sctx, func() (Gathered, error) { return src.Gather(sctx, r.h, &sc) })
		ch <- result{g, err}
	}()
	var res result
	if hideAfter > 0 {
		t := time.NewTimer(hideAfter)
		select {
		case res = <-ch:
			t.Stop()
		case <-t.C:
			// Too slow: the popup is hidden and the source left out of this
			// request, as in ddc.vim.
			r.hideStale()
			if ctx.Err() == nil {
				log.Printf("cmp: source %s: no answer within hideTimeout (%v)", name, hideAfter)
			}
			return
		}
	} else {
		res = <-ch
	}
	if res.err != nil {
		if ctx.Err() == nil { // a superseded request is not worth a log line
			log.Printf("cmp: source %s: %v", name, res.err)
		}
		return
	}
	g := mapStart(res.g)
	if ctx.Err() == nil {
		remember(g)
	}
	if items, ok := process(g); ok {
		r.store(i, g.Start, items, true)
	}
}

// filterItems runs what a source gathered through its filters: the keyword
// lengths, the matchers (on matcherKey), the sorters, maxItems and the
// converters. raw is not modified.
func (r *runner) filterItems(ctx context.Context, name string, opt SourceOption, complete string, raw []Item, timeout time.Duration) ([]Item, error) {
	minKw, maxKw := deref(opt.MinKeywordLength, 0), deref(opt.MaxKeywordLength, 0)
	items := make([]Item, 0, len(raw))
	for _, it := range raw {
		if minKw > 0 || maxKw > 0 {
			n := utf8.RuneCountInString(it.Word)
			if (minKw > 0 && n < minKw) || (maxKw > 0 && n > maxKw) {
				continue
			}
		}
		it.Source = name
		if it.Menu == "" {
			it.Menu = opt.Mark
		}
		items = append(items, it)
	}

	fs := Filters{Matchers: opt.Matchers, Sorters: opt.Sorters, Converters: opt.Converters}
	if opt.DynamicFilters != nil {
		d, err := sdk.Guard(ctx, func() (Filters, error) {
			return opt.DynamicFilters(ctx, r.h, DynamicFiltersArgs{Context: r.c, Source: name, Complete: complete, Items: slices.Clone(items)})
		})
		if err != nil {
			log.Printf("cmp: source %s: dynamicFilters: %v", name, err)
		} else {
			if d.Matchers != nil {
				fs.Matchers = d.Matchers
			}
			if d.Sorters != nil {
				fs.Sorters = d.Sorters
			}
			if d.Converters != nil {
				fs.Converters = d.Converters
			}
		}
	}
	ignoreCase := deref(opt.IgnoreCase, false)

	// matcherKey: the matchers see the attribute as FilterText; each item
	// remembers its own through Ref, which the engine sets only later.
	var saved []string
	if opt.MatcherKey != "" {
		saved = make([]string, len(items))
		for k := range items {
			saved[k] = items[k].FilterText
			items[k].FilterText = attribute(items[k], opt.MatcherKey)
			items[k].Ref = strconv.Itoa(k)
		}
	}
	items, err := r.chain(ctx, complete, items, fs.Matchers, ignoreCase, timeout)
	if err != nil {
		return nil, err
	}
	if saved != nil {
		for k := range items {
			if n, err := strconv.Atoi(items[k].Ref); err == nil && n < len(saved) {
				items[k].FilterText = saved[n]
			}
			items[k].Ref = ""
		}
	}
	// The cap applies after sorting, so the best candidates survive.
	if items, err = r.chain(ctx, complete, items, fs.Sorters, ignoreCase, timeout); err != nil {
		return nil, err
	}
	if m := deref(opt.MaxItems, 0); m > 0 && len(items) > m {
		items = items[:m]
	}
	return r.chain(ctx, complete, items, fs.Converters, ignoreCase, timeout)
}

// attribute is the value of a complete-item attribute (matcherKey).
func attribute(it Item, key string) string {
	switch key {
	case "abbr":
		return it.Abbr
	case "menu":
		return it.Menu
	case "kind":
		return it.Kind
	case "info":
		return it.Info
	}
	return it.Word
}

// chain applies filters in order. A failing filter aborts the chain with its
// error.
func (r *runner) chain(ctx context.Context, complete string, items []Item, names []string, ignoreCase bool, timeout time.Duration) ([]Item, error) {
	for _, n := range names {
		if len(items) == 0 {
			return items, nil
		}
		f, err := r.e.filter(r.cfg, r.al, n)
		if err != nil {
			return items, err
		}
		fctx, cancel := context.WithTimeout(ctx, timeout)
		out, err := sdk.Guard(fctx, func() ([]Item, error) {
			return f.Filter(fctx, FilterInput{Context: r.c, Complete: complete, Items: items, Host: r.h, IgnoreCase: ignoreCase})
		})
		cancel()
		if err != nil {
			return items, fmt.Errorf("filter %s: %w", n, err)
		}
		items = out
	}
	return items, nil
}

// prevResult is what a source gathered, kept for the next keystrokes.
type prevResult struct {
	bufnr, lnum int
	mode        string // Mode and CmdType
	kwStart     int    // the keyword start it was gathered for
	start       int    // Gathered.Start
	prefix      string // Input[:start] then
	items       []Item
	incomplete  bool
	at          time.Time
}

// cached returns the kept result of a source when it is still good for c:
// the same buffer, line and mode, the same keyword start, and the text before
// the start unchanged. Results that are not are forgotten.
func (e *engine) cached(key string, c *Context, kw int, cacheTimeout int) *prevResult {
	e.cacheMu.Lock()
	defer e.cacheMu.Unlock()
	p := e.prev[key]
	if p == nil {
		return nil
	}
	ok := p.bufnr == c.Bufnr && p.lnum == c.Lnum && p.mode == c.Mode+c.CmdType &&
		p.kwStart == kw && p.start <= len(c.Input) && c.Input[:p.start] == p.prefix
	if ok && cacheTimeout > 0 && time.Since(p.at) > time.Duration(cacheTimeout)*time.Second {
		ok = false
	}
	if !ok {
		delete(e.prev, key)
		return nil
	}
	return p
}

func (e *engine) cache(key string, p *prevResult) {
	e.cacheMu.Lock()
	defer e.cacheMu.Unlock()
	e.prev[key] = p
}

func (e *engine) uncache(key string) {
	e.cacheMu.Lock()
	defer e.cacheMu.Unlock()
	delete(e.prev, key)
}

// updateItems replaces the kept items of the source name (every set of
// params of it), as ddc#update_items() does. It reports whether there were
// any.
func (e *engine) updateItems(name string, items []Item) bool {
	e.cacheMu.Lock()
	defer e.cacheMu.Unlock()
	found := false
	for k, p := range e.prev {
		if k == name || (len(k) > len(name) && k[len(k)-len(name)-1:] == "\x00"+name) {
			p.items = slices.Clone(items)
			p.incomplete = false
			found = true
		}
	}
	return found
}

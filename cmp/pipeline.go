package cmp

import (
	"context"
	"fmt"
	"log"
	"sync"
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
	timeout time.Duration

	mu    sync.Mutex
	slots []slot
	kick  chan struct{}
}

// run gathers, filters and merges, calling show as results arrive. Without a
// StreamSource among the enabled sources show is called once, when every
// source has answered, exactly like a plain gather. With one, it is called
// whenever something new is there, at most every PartialInterval.
func (e *engine) run(ctx context.Context, h sdk.Host, c *Context, cfg Config, show showFunc) error {
	r := &runner{
		e: e, h: h, c: c, cfg: cfg, timeout: cfg.timeout(),
		slots: make([]slot, len(cfg.Sources)),
		kick:  make(chan struct{}, 1),
	}
	partial := false
	for i, name := range cfg.Sources {
		r.slots[i].name = name
		if _, ok := e.b.sources[name].(StreamSource); ok {
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

// merge combines the slots. Sources may start at different columns;
// everything is moved to the leftmost one by prepending the text in between
// to the words.
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
		merged, err = r.e.chain(context.Background(), r.h, r.c, r.c.Input[minStart:], merged, r.cfg.PostFilters, r.timeout)
		if err != nil {
			log.Printf("cmp: postFilters: %v", err)
		}
	}
	return merged, minStart, pending
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

func (r *runner) gatherOne(ctx context.Context, i int) {
	e, c, cfg := r.e, r.c, r.cfg
	name := r.slots[i].name
	src := e.b.sources[name]
	opt := cfg.sourceOption(name)
	defer r.finish(i)

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

	// MinAutoCompleteLength, unless the user asked or forceCompletionPattern
	// matches (ddc.vim's rule). The word before the cursor decides whether
	// the source is asked at all; what the source answers is checked again
	// below, against its own start.
	force := c.Manual
	if !force && opt.ForceCompletionPattern != "" {
		re, err := e.keyword(opt.ForceCompletionPattern)
		if err != nil {
			log.Printf("cmp: source %s: %v", name, err)
			return
		}
		force = re.MatchString(c.Input)
	}
	minLen := deref(opt.MinAutoCompleteLength, 1)
	short := func(complete string) bool {
		return !force && utf8.RuneCountInString(complete) < minLen
	}
	if short(c.Input[sc.KeywordStart():]) {
		return
	}

	sctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	if err := e.ensureInit(sctx, r.h, name, src, r.timeout); err != nil {
		if ctx.Err() == nil {
			log.Printf("cmp: source %s: init: %v", name, err)
		}
		return
	}

	// process turns what the source returned into this source's slot.
	process := func(g Gathered) ([]Item, bool) {
		if g.Start < 0 || g.Start > len(c.Input) {
			log.Printf("cmp: source %s: start %d is outside the input", name, g.Start)
			return nil, false
		}
		complete := c.Input[g.Start:]
		if short(complete) {
			return nil, true
		}
		items := make([]Item, len(g.Items))
		copy(items, g.Items)
		for k := range items {
			items[k].Source = name
			if items[k].Menu == "" {
				items[k].Menu = opt.Mark
			}
		}
		// The cap applies after sorting, so the best candidates survive.
		items, err := e.chain(sctx, r.h, c, complete, items, opt.Matchers, r.timeout)
		if err == nil {
			items, err = e.chain(sctx, r.h, c, complete, items, opt.Sorters, r.timeout)
		}
		if err == nil {
			if m := deref(opt.MaxItems, 0); m > 0 && len(items) > m {
				items = items[:m]
			}
			items, err = e.chain(sctx, r.h, c, complete, items, opt.Converters, r.timeout)
		}
		if err != nil {
			log.Printf("cmp: source %s: %v", name, err)
			return nil, false
		}
		return items, true
	}

	if ss, ok := src.(StreamSource); ok {
		_, err := sdk.Guard(sctx, func() (struct{}, error) {
			return struct{}{}, ss.GatherStream(sctx, r.h, &sc, func(g Gathered) error {
				if err := sctx.Err(); err != nil {
					return err
				}
				if items, ok := process(g); ok {
					r.store(i, g.Start, items, false)
				}
				return nil
			})
		})
		if err != nil && ctx.Err() == nil {
			log.Printf("cmp: source %s: %v", name, err)
		}
		return // what was emitted stays
	}

	g, err := sdk.Guard(sctx, func() (Gathered, error) { return src.Gather(sctx, r.h, &sc) })
	if err != nil {
		if ctx.Err() == nil { // a superseded request is not worth a log line
			log.Printf("cmp: source %s: %v", name, err)
		}
		return
	}
	if items, ok := process(g); ok {
		r.store(i, g.Start, items, true)
	}
}

// chain applies filters in order. A failing filter aborts the chain with its
// error.
func (e *engine) chain(ctx context.Context, h sdk.Host, c *Context, complete string, items []Item, names []string, timeout time.Duration) ([]Item, error) {
	for _, n := range names {
		if len(items) == 0 {
			return items, nil
		}
		f := e.b.filters[n]
		fctx, cancel := context.WithTimeout(ctx, timeout)
		out, err := sdk.Guard(fctx, func() ([]Item, error) {
			return f.Filter(fctx, FilterInput{Context: c, Complete: complete, Items: items, Host: h})
		})
		cancel()
		if err != nil {
			return items, fmt.Errorf("filter %s: %w", n, err)
		}
		items = out
	}
	return items, nil
}

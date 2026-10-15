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

type gatherResult struct {
	name  string
	start int
	items []Item
	err   error
}

// run gathers, filters and merges. It returns the items and the byte offset in
// c.Input where they start.
func (e *engine) run(ctx context.Context, h sdk.Host, c *Context, cfg Config) ([]Item, int, error) {
	timeout := cfg.timeout()

	results := make([]gatherResult, len(cfg.Sources))
	var wg sync.WaitGroup
	for i, name := range cfg.Sources {
		wg.Add(1)
		go func() {
			defer wg.Done()
			results[i] = e.gatherOne(ctx, h, c, cfg, name, timeout)
		}()
	}
	wg.Wait()

	// Sources may start at different columns. Everything is moved to the
	// leftmost one by prepending the text in between to the words.
	minStart := -1
	for _, r := range results {
		if r.err == nil && len(r.items) > 0 && (minStart < 0 || r.start < minStart) {
			minStart = r.start
		}
	}
	if minStart < 0 {
		return nil, 0, nil
	}
	var merged []Item
	for _, r := range results {
		if r.err != nil || len(r.items) == 0 {
			continue
		}
		prefix := c.Input[minStart:r.start]
		for _, it := range r.items {
			if prefix != "" {
				it.Word = prefix + it.Word
			}
			merged = append(merged, it)
		}
	}

	if len(cfg.PostFilters) > 0 {
		var err error
		merged, err = e.chain(ctx, c, c.Input[minStart:], merged, cfg.PostFilters, timeout)
		if err != nil {
			log.Printf("cmp: postFilters: %v", err)
		}
	}
	return merged, minStart, nil
}

func (e *engine) gatherOne(ctx context.Context, h sdk.Host, c *Context, cfg Config, name string, timeout time.Duration) gatherResult {
	res := gatherResult{name: name}
	src := e.b.sources[name]
	opt := cfg.sourceOption(name)

	sctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	g, err := sdk.Guard(sctx, func() (Gathered, error) { return src.Gather(sctx, h, c) })
	if err != nil {
		if ctx.Err() == nil { // a superseded request is not worth a log line
			log.Printf("cmp: source %s: %v", name, err)
		}
		res.err = err
		return res
	}
	if g.Start < 0 || g.Start > len(c.Input) {
		res.err = fmt.Errorf("start %d is outside the input", g.Start)
		log.Printf("cmp: source %s: %v", name, res.err)
		return res
	}
	complete := c.Input[g.Start:]
	if !c.Manual && utf8.RuneCountInString(complete) < deref(opt.MinAutoCompleteLength, 1) {
		return res
	}

	items := make([]Item, len(g.Items))
	copy(items, g.Items)
	for i := range items {
		items[i].Source = name
		if items[i].Menu == "" {
			items[i].Menu = opt.Mark
		}
	}
	// The cap applies after sorting, so the best candidates survive.
	items, err = e.chain(ctx, c, complete, items, opt.Matchers, timeout)
	if err == nil {
		items, err = e.chain(ctx, c, complete, items, opt.Sorters, timeout)
	}
	if err == nil {
		if m := deref(opt.MaxItems, 0); m > 0 && len(items) > m {
			items = items[:m]
		}
		items, err = e.chain(ctx, c, complete, items, opt.Converters, timeout)
	}
	if err != nil {
		log.Printf("cmp: source %s: %v", name, err)
		res.err = err
		return res
	}
	res.start, res.items = g.Start, items
	return res
}

// chain applies filters in order. A failing filter aborts the chain with its
// error.
func (e *engine) chain(ctx context.Context, c *Context, complete string, items []Item, names []string, timeout time.Duration) ([]Item, error) {
	for _, n := range names {
		if len(items) == 0 {
			return items, nil
		}
		f := e.b.filters[n]
		fctx, cancel := context.WithTimeout(ctx, timeout)
		out, err := sdk.Guard(fctx, func() ([]Item, error) {
			return f.Filter(fctx, FilterInput{Context: c, Complete: complete, Items: items})
		})
		cancel()
		if err != nil {
			return items, fmt.Errorf("filter %s: %w", n, err)
		}
		items = out
	}
	return items, nil
}

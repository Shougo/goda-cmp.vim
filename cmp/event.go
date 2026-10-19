package cmp

import (
	"context"
	"fmt"
	"log"
	"regexp"
	"slices"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/Shougo/goda.vim/sdk"
)

// EventSpec is what a source wants to hear about: autocmd events (BufEnter,
// BufDelete, ...) for buffers of some filetypes. Vim filters by filetype
// before anything is sent, so a source that names its filetypes costs
// nothing in other buffers.
type EventSpec struct {
	Events []string `json:"events"`
	// Filetypes the events are wanted for; empty means every filetype. A
	// dotted filetype ("cpp.doxygen") matches when any part does.
	Filetypes []string `json:"filetypes"`
}

// Event is one autocmd event of a buffer.
type Event struct {
	// Seq numbers the events in the order Vim saw them, from 1 for each
	// start of the plugin.
	Seq      int64  `json:"seq"`
	Name     string `json:"name"` // the autocmd event
	Bufnr    int    `json:"bufnr"`
	Tick     int64  `json:"tick"` // b:changedtick
	Filetype string `json:"filetype"`
	Buftype  string `json:"buftype"`
	// Path is the full path of the buffer's file, "" for a buffer without
	// a name.
	Path string `json:"path"`
}

// EventHandler is implemented by sources that act on buffer events: a
// language server source starts its server when a file of its filetype is
// opened, and closes documents when their buffers go away.
//
// Events is asked once, when the plugin starts. OnEvent receives the events
// of one source one at a time, in the order Vim saw them, bounded by
// sourceTimeout. Errors are logged. When goda-cmp is enabled, the current
// buffer gets a BufEnter (if the source wants BufEnter), since its real one
// happened before.
type EventHandler interface {
	Events() []EventSpec
	OnEvent(ctx context.Context, h sdk.Host, ev Event) error
}

var eventName = regexp.MustCompile(`^[A-Z][A-Za-z]*$`)

// eventGap is how long an event waits for one with a smaller Seq that has
// not arrived (requests are served concurrently, so notifications sent in
// order may start in another order).
const eventGap = 50 * time.Millisecond

type eventItem struct {
	h  sdk.Host
	ev Event
}

// events orders the events and hands them to one queue per source.
type events struct {
	e     *engine
	specs map[string][]EventSpec // by source, in sourceOrder

	mu      sync.Mutex
	next    int64 // the Seq expected next; Vim numbers from 1 per start
	pending map[int64]eventItem
	timer   *time.Timer
	queues  map[string]chan eventItem
	closed  bool
}

func newEvents(e *engine) *events {
	ev := &events{e: e, next: 1, specs: map[string][]EventSpec{}, pending: map[int64]eventItem{}, queues: map[string]chan eventItem{}}
	for _, name := range e.b.sourceOrder {
		if h, ok := e.b.sources[name].(EventHandler); ok {
			ev.specs[name] = h.Events()
		}
	}
	return ev
}

// checkEvents reports event names that cannot be autocmd events.
func (b *Builder) checkEvents() []string {
	var errs []string
	for _, name := range b.sourceOrder {
		h, ok := b.sources[name].(EventHandler)
		if !ok {
			continue
		}
		for _, s := range h.Events() {
			for _, n := range s.Events {
				if !eventName.MatchString(n) {
					errs = append(errs, fmt.Sprintf("source %q: bad event name %q", name, n))
				}
			}
		}
	}
	return errs
}

// table is what the Vim side needs to set up its autocmds: for each event,
// the filetypes to send it for ([] for all).
func (ev *events) table() []map[string]any {
	fts := map[string]map[string]bool{} // event -> filetypes; nil map = all
	all := map[string]bool{}
	for _, specs := range ev.specs {
		for _, s := range specs {
			for _, n := range s.Events {
				if len(s.Filetypes) == 0 {
					all[n] = true
				}
				if fts[n] == nil {
					fts[n] = map[string]bool{}
				}
				for _, ft := range s.Filetypes {
					fts[n][ft] = true
				}
			}
		}
	}
	names := make([]string, 0, len(fts))
	for n := range fts {
		names = append(names, n)
	}
	sort.Strings(names)
	out := make([]map[string]any, 0, len(names))
	for _, n := range names {
		list := []string{}
		if !all[n] {
			for ft := range fts[n] {
				list = append(list, ft)
			}
			sort.Strings(list)
		}
		out = append(out, map[string]any{"event": n, "filetypes": list})
	}
	return out
}

func (s EventSpec) matches(ev Event) bool {
	if !slices.Contains(s.Events, ev.Name) {
		return false
	}
	if len(s.Filetypes) == 0 {
		return true
	}
	return slices.ContainsFunc(strings.Split(ev.Filetype, "."), func(ft string) bool {
		return slices.Contains(s.Filetypes, ft)
	})
}

// add takes an event from Vim. Events are dispatched in Seq order; a gap
// is waited for eventGap at most.
func (ev *events) add(h sdk.Host, e Event) {
	ev.mu.Lock()
	defer ev.mu.Unlock()
	if ev.closed {
		return
	}
	if e.Seq < ev.next { // late: a gap was given up on
		ev.dispatch(eventItem{h, e})
		return
	}
	ev.pending[e.Seq] = eventItem{h, e}
	ev.flush()
	if len(ev.pending) > 0 && ev.timer == nil {
		ev.timer = time.AfterFunc(eventGap, ev.giveUp)
	}
}

func (ev *events) flush() {
	for {
		it, ok := ev.pending[ev.next]
		if !ok {
			break
		}
		delete(ev.pending, ev.next)
		ev.dispatch(it)
		ev.next++
	}
	if len(ev.pending) == 0 && ev.timer != nil {
		ev.timer.Stop()
		ev.timer = nil
	}
}

// giveUp skips the missing events and goes on with the ones that came.
func (ev *events) giveUp() {
	ev.mu.Lock()
	defer ev.mu.Unlock()
	ev.timer = nil
	if ev.closed || len(ev.pending) == 0 {
		return
	}
	lowest := int64(-1)
	for s := range ev.pending {
		if lowest < 0 || s < lowest {
			lowest = s
		}
	}
	ev.next = lowest
	ev.flush()
	if len(ev.pending) > 0 {
		ev.timer = time.AfterFunc(eventGap, ev.giveUp)
	}
}

// dispatch puts the event in the queue of every source that wants it.
// Called with mu held.
func (ev *events) dispatch(it eventItem) {
	for _, name := range ev.e.b.sourceOrder {
		specs, ok := ev.specs[name]
		if !ok || !slices.ContainsFunc(specs, func(s EventSpec) bool { return s.matches(it.ev) }) {
			continue
		}
		q := ev.queues[name]
		if q == nil {
			q = make(chan eventItem, 256)
			ev.queues[name] = q
			go ev.serve(name, q)
		}
		select {
		case q <- it:
		default:
			log.Printf("cmp: source %s: too many events queued, dropping %s", name, it.ev.Name)
		}
	}
}

// serve delivers the events of one source, one at a time.
func (ev *events) serve(name string, q chan eventItem) {
	h := ev.e.b.sources[name].(EventHandler)
	for it := range q {
		timeout := ev.e.b.resolve(it.ev.Filetype, Patches{}).timeout()
		ctx, cancel := context.WithTimeout(context.Background(), timeout)
		_, err := sdk.Guard(ctx, func() (struct{}, error) { return struct{}{}, h.OnEvent(ctx, it.h, it.ev) })
		cancel()
		if err != nil {
			log.Printf("cmp: source %s: %s: %v", name, it.ev.Name, err)
		}
	}
}

func (ev *events) close() {
	ev.mu.Lock()
	defer ev.mu.Unlock()
	if ev.closed {
		return
	}
	ev.closed = true
	if ev.timer != nil {
		ev.timer.Stop()
	}
	for _, q := range ev.queues {
		close(q)
	}
}

func (e *engine) eventsHandler(context.Context, sdk.Host, []any) (any, error) {
	return e.ev.table(), nil
}

func (e *engine) event(_ context.Context, h sdk.Host, params []any) (any, error) {
	if len(params) == 0 {
		return nil, fmt.Errorf("cmp.event needs an event dictionary")
	}
	var ev Event
	if err := sdk.Decode(params[0], &ev); err != nil {
		return nil, err
	}
	e.ev.add(h, ev)
	return nil, nil
}

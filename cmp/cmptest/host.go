// Package cmptest provides a fake editor for testing sources, filters and UIs
// without Vim.
package cmptest

import (
	"context"
	"fmt"
	"sync"

	"github.com/Shougo/goda.vim/sdk"
)

// Call is one recorded Host.Call or Host.Notify.
type Call struct {
	Fn   string
	Args []any
	// Notify is set for a Host.Notify.
	Notify bool
}

// Host is an sdk.Host whose Vim functions are Go functions. Eval understands
// calls of those functions (see Eval); Cmd always fails.
type Host struct {
	// Funcs maps a Vim function name to its fake. A call to a missing name
	// fails like E117 does.
	Funcs map[string]func(args ...any) (any, error)

	mu    sync.Mutex
	calls []Call
}

func (h *Host) Meta() sdk.Meta { return sdk.Meta{Host: sdk.HostVim} }

func (h *Host) Call(_ context.Context, fn string, args ...any) (any, error) {
	h.mu.Lock()
	h.calls = append(h.calls, Call{Fn: fn, Args: args})
	f := h.Funcs[fn]
	h.mu.Unlock()
	if f == nil {
		return nil, fmt.Errorf("E117: Unknown function: %s", fn)
	}
	return f(args...)
}

// Notify runs the fake like Call does and drops its result. As in the
// editor, an error of the function is not returned.
func (h *Host) Notify(_ context.Context, fn string, args ...any) error {
	h.mu.Lock()
	h.calls = append(h.calls, Call{Fn: fn, Args: args, Notify: true})
	f := h.Funcs[fn]
	h.mu.Unlock()
	if f != nil {
		_, _ = f(args...)
	}
	return nil
}

func (h *Host) Cmd(context.Context, string) error { return fmt.Errorf("cmptest: Cmd") }

// Calls returns what was called so far.
func (h *Host) Calls() []Call {
	h.mu.Lock()
	defer h.mu.Unlock()
	return append([]Call(nil), h.calls...)
}

// Lines makes a Host whose getbufline returns the given lines, honouring the
// first/last arguments like Vim does.
func Lines(lines ...string) *Host {
	return &Host{Funcs: map[string]func(...any) (any, error){
		"getbufline": func(a ...any) (any, error) {
			first, last := toInt(a[1]), toInt(a[2])
			if first < 1 {
				first = 1
			}
			if last > len(lines) {
				last = len(lines)
			}
			out := []any{}
			for i := first; i <= last; i++ {
				out = append(out, lines[i-1])
			}
			return out, nil
		},
	}}
}

func toInt(v any) int {
	switch n := v.(type) {
	case int:
		return n
	case int64:
		return int(n)
	case float64:
		return int(n)
	}
	return 0
}

// Host must stay an sdk.Host when goda.vim changes.
var _ sdk.Host = (*Host)(nil)

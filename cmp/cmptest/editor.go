package cmptest

import (
	"fmt"
	"path/filepath"
	"slices"
	"strings"
	"sync"
)

// Buffer is a buffer of an Editor.
type Buffer struct {
	Name    string // the file name, as bufname() returns it
	Buftype string // 'buftype'
	Lines   []string
}

// Editor is a fake Vim with buffers, one window and a cursor, for sources
// that read and change buffers. Host returns its sdk.Host; it serves these
// functions the way Vim does (the cases sources use, not every variant):
//
//	bufnr('%')  bufname(nr)  fnamemodify(name, ':p')  getbufvar(nr, '&buftype')
//	getbufline(nr, first [, last])  setbufline(nr, lnum, text)
//	appendbufline(nr, lnum, text)  deletebufline(nr, first [, last])
//	getline(lnum)  setline(lnum, text)  line('.')  line('$')  col('.')
//	cursor(lnum, col)
//
// A buffer argument is a number or '%'; a line number may be '.' or '$'.
// More functions can be added to the Host's Funcs.
type Editor struct {
	mu      sync.Mutex
	buffers map[int]*Buffer
	current int
	cursor  [2]int // 1-based line, 1-based byte column
	host    *Host
}

// NewEditor returns an editor whose current buffer (number 1) is named name
// and holds lines, with the cursor on the first character.
func NewEditor(name string, lines ...string) *Editor {
	e := &Editor{buffers: map[int]*Buffer{}, current: 1, cursor: [2]int{1, 1}}
	e.buffers[1] = &Buffer{Name: name, Lines: append([]string(nil), lines...)}
	e.host = &Host{Funcs: e.funcs()}
	return e
}

// Host is the editor as the source sees it.
func (e *Editor) Host() *Host { return e.host }

// AddBuffer adds a buffer under the next free number and returns that number.
func (e *Editor) AddBuffer(b Buffer) int {
	e.mu.Lock()
	defer e.mu.Unlock()
	n := 1
	for e.buffers[n] != nil {
		n++
	}
	b.Lines = append([]string(nil), b.Lines...)
	e.buffers[n] = &b
	return n
}

// Lines returns a copy of a buffer's lines.
func (e *Editor) Lines(bufnr int) []string {
	e.mu.Lock()
	defer e.mu.Unlock()
	if b := e.buffers[bufnr]; b != nil {
		return append([]string(nil), b.Lines...)
	}
	return nil
}

// SetLines replaces a buffer's lines.
func (e *Editor) SetLines(bufnr int, lines ...string) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if b := e.buffers[bufnr]; b != nil {
		b.Lines = append([]string(nil), lines...)
	}
}

// Cursor returns the cursor: 1-based line and byte column, like line('.')
// and col('.').
func (e *Editor) Cursor() (lnum, col int) {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.cursor[0], e.cursor[1]
}

// SetCursor moves the cursor (1-based line and byte column).
func (e *Editor) SetCursor(lnum, col int) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.cursor = [2]int{lnum, col}
}

// SetCurrent makes another buffer the current one.
func (e *Editor) SetCurrent(bufnr int) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.current = bufnr
}

func (e *Editor) funcs() map[string]func(...any) (any, error) {
	// fn wraps a function that runs with the lock held and its arguments
	// checked for count.
	fn := func(minArgs int, f func(a []any) (any, error)) func(...any) (any, error) {
		return func(a ...any) (any, error) {
			if len(a) < minArgs {
				return nil, fmt.Errorf("E119: Not enough arguments")
			}
			e.mu.Lock()
			defer e.mu.Unlock()
			return f(a)
		}
	}
	return map[string]func(...any) (any, error){
		"bufnr": fn(0, func(a []any) (any, error) {
			if len(a) == 0 || a[0] == "%" || a[0] == "" {
				return int64(e.current), nil
			}
			if b, _ := e.buf(a[0]); b != nil {
				return int64(toInt(a[0])), nil
			}
			return int64(-1), nil
		}),
		"bufname": fn(0, func(a []any) (any, error) {
			var arg any = "%"
			if len(a) > 0 {
				arg = a[0]
			}
			b, _ := e.buf(arg)
			if b == nil {
				return "", nil
			}
			return b.Name, nil
		}),
		"fnamemodify": fn(2, func(a []any) (any, error) {
			name, _ := a[0].(string)
			if a[1] != ":p" {
				return nil, fmt.Errorf("cmptest: fnamemodify supports ':p' only")
			}
			if name == "" {
				return "", nil
			}
			return filepath.Abs(name)
		}),
		"getbufvar": fn(2, func(a []any) (any, error) {
			b, err := e.buf(a[0])
			if err != nil {
				return "", nil // Vim answers the default for a missing buffer
			}
			if a[1] == "&buftype" {
				return b.Buftype, nil
			}
			return nil, fmt.Errorf("cmptest: getbufvar supports '&buftype' only")
		}),
		"getbufline": fn(2, func(a []any) (any, error) {
			b, err := e.buf(a[0])
			if err != nil {
				return []any{}, nil
			}
			first := e.lnum(b, a[1])
			last := first
			if len(a) > 2 {
				last = e.lnum(b, a[2])
			}
			out := []any{}
			for l := max(first, 1); l <= last && l <= len(b.Lines); l++ {
				out = append(out, b.Lines[l-1])
			}
			return out, nil
		}),
		"getline": fn(1, func(a []any) (any, error) {
			b := e.buffers[e.current]
			l := e.lnum(b, a[0])
			if l < 1 || l > len(b.Lines) {
				return "", nil
			}
			return b.Lines[l-1], nil
		}),
		"setbufline": fn(3, func(a []any) (any, error) {
			b, err := e.buf(a[0])
			if err != nil {
				return int64(1), nil
			}
			return e.set(b, e.lnum(b, a[1]), a[2])
		}),
		"setline": fn(2, func(a []any) (any, error) {
			b := e.buffers[e.current]
			return e.set(b, e.lnum(b, a[0]), a[1])
		}),
		"appendbufline": fn(3, func(a []any) (any, error) {
			b, err := e.buf(a[0])
			if err != nil {
				return int64(1), nil
			}
			l := e.lnum(b, a[1])
			if l < 0 || l > len(b.Lines) {
				return nil, fmt.Errorf("E966: Invalid line number: %d", l)
			}
			text := texts(a[2])
			b.Lines = slices.Insert(b.Lines, l, text...)
			if b == e.buffers[e.current] && e.cursor[0] > l {
				e.cursor[0] += len(text)
			}
			return int64(0), nil
		}),
		"deletebufline": fn(2, func(a []any) (any, error) {
			b, err := e.buf(a[0])
			if err != nil {
				return int64(1), nil
			}
			first := e.lnum(b, a[1])
			last := first
			if len(a) > 2 {
				last = e.lnum(b, a[2])
			}
			if first < 1 || last > len(b.Lines) || first > last {
				return int64(1), nil
			}
			b.Lines = slices.Delete(b.Lines, first-1, last)
			if len(b.Lines) == 0 {
				b.Lines = []string{""}
			}
			if b == e.buffers[e.current] && e.cursor[0] > last {
				e.cursor[0] -= last - first + 1
			}
			return int64(0), nil
		}),
		"line": fn(1, func(a []any) (any, error) {
			return int64(e.lnum(e.buffers[e.current], a[0])), nil
		}),
		"col": fn(1, func(a []any) (any, error) {
			if a[0] != "." {
				return nil, fmt.Errorf("cmptest: col supports '.' only")
			}
			return int64(e.cursor[1]), nil
		}),
		"cursor": fn(2, func(a []any) (any, error) {
			e.cursor = [2]int{toInt(a[0]), toInt(a[1])}
			return int64(0), nil
		}),
	}
}

// buf finds a buffer by number or '%'.
func (e *Editor) buf(arg any) (*Buffer, error) {
	n := e.current
	if s, ok := arg.(string); !ok || (s != "%" && s != "") {
		n = toInt(arg)
	}
	b := e.buffers[n]
	if b == nil {
		return nil, fmt.Errorf("no buffer %v", arg)
	}
	return b, nil
}

// lnum reads a line number: a number, '.' or '$'.
func (e *Editor) lnum(b *Buffer, arg any) int {
	switch arg {
	case ".":
		return e.cursor[0]
	case "$":
		return len(b.Lines)
	}
	return toInt(arg)
}

// set is setbufline(): text (a string or a list) replaces lines from l on,
// appending at the end of the buffer.
func (e *Editor) set(b *Buffer, l int, text any) (any, error) {
	ts := texts(text)
	if l < 1 || l > len(b.Lines)+1 {
		return int64(1), nil
	}
	for i, t := range ts {
		if l-1+i < len(b.Lines) {
			b.Lines[l-1+i] = t
		} else {
			b.Lines = append(b.Lines, t)
		}
	}
	return int64(0), nil
}

// texts reads a string or a list of strings.
func texts(v any) []string {
	switch x := v.(type) {
	case string:
		return []string{x}
	case []string:
		return x
	case []any:
		out := make([]string, len(x))
		for i, s := range x {
			out[i] = fmt.Sprint(s)
			if str, ok := s.(string); ok {
				out[i] = str
			}
		}
		return out
	}
	return []string{strings.TrimSpace(fmt.Sprint(v))}
}

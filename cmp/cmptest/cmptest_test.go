package cmptest_test

import (
	"context"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/Shougo/goda-cmp.vim/cmp/cmptest"
)

func TestEval(t *testing.T) {
	e := cmptest.NewEditor("/tmp/a.go", "one", "two", "three")
	h := e.Host()
	ctx := context.Background()
	got, err := h.Eval(ctx, `[fnamemodify(bufname(1), ':p'), getbufvar(1, '&buftype'), getbufline(1, 2, '$'), -3, "a\tb", 'it''s', []]`)
	if err != nil {
		t.Fatal(err)
	}
	abs, _ := filepath.Abs("/tmp/a.go")
	want := []any{abs, "", []any{"two", "three"}, int64(-3), "a\tb", "it's", []any{}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %#v\nwant %#v", got, want)
	}
	for _, bad := range []string{"1 + 2", "nosuchfunc()", "[1, 2", "g:var", "'open"} {
		if _, err := h.Eval(ctx, bad); err == nil {
			t.Errorf("%q: no error", bad)
		}
	}
}

// The buffer functions behave like Vim's (checked against Vim 9.1),
// including the cursor following the lines when lines above it come and go.
func TestEditor(t *testing.T) {
	e := cmptest.NewEditor("f.txt", "a", "b", "c")
	h := e.Host()
	ctx := context.Background()
	call := func(fn string, args ...any) any {
		t.Helper()
		v, err := h.Call(ctx, fn, args...)
		if err != nil {
			t.Fatalf("%s: %v", fn, err)
		}
		return v
	}
	e.SetCursor(3, 1)
	call("appendbufline", 1, 0, []string{"x"})
	if l, _ := e.Cursor(); l != 4 {
		t.Errorf("after append above: line %d, want 4", l)
	}
	call("deletebufline", "%", 1)
	if l, _ := e.Cursor(); l != 3 {
		t.Errorf("after delete above: line %d, want 3", l)
	}
	call("setbufline", 1, 3, []any{"C", "D"}) // replaces 3, appends 4
	if got, want := e.Lines(1), []string{"a", "b", "C", "D"}; !reflect.DeepEqual(got, want) {
		t.Errorf("lines = %q, want %q", got, want)
	}
	if v := call("getline", "."); v != "C" {
		t.Errorf("getline('.') = %v", v)
	}
	if v := call("line", "$"); v != int64(4) {
		t.Errorf("line('$') = %v", v)
	}
	call("cursor", 2, 2)
	if v, _ := h.Eval(ctx, "[bufnr('%'), line('.'), col('.')]"); !reflect.DeepEqual(v, []any{int64(1), int64(2), int64(2)}) {
		t.Errorf("cursor = %v", v)
	}
	n := e.AddBuffer(cmptest.Buffer{Name: "help.txt", Buftype: "help", Lines: []string{"h"}})
	if v := call("getbufvar", n, "&buftype"); v != "help" {
		t.Errorf("buftype = %v", v)
	}
}

func TestNotify(t *testing.T) {
	e := cmptest.NewEditor("f.txt", "a")
	h := e.Host()
	ctx := context.Background()
	if err := h.Notify(ctx, "setbufline", 1, 1, "b"); err != nil {
		t.Fatal(err)
	}
	if err := h.Notify(ctx, "no_such_function"); err != nil {
		t.Errorf("an error of the function came back: %v", err)
	}
	if got := e.Lines(1); !reflect.DeepEqual(got, []string{"b"}) {
		t.Errorf("lines = %q", got)
	}
	calls := h.Calls()
	if len(calls) != 2 || !calls[0].Notify || calls[0].Fn != "setbufline" {
		t.Errorf("calls = %+v", calls)
	}
}

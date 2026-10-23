package cmdlinehistory

import (
	"context"
	"reflect"
	"testing"

	"github.com/Shougo/goda-cmp.vim/cmp"
	"github.com/Shougo/goda-cmp.vim/cmp/cmptest"
)

// fakeHost is an editor whose history of type ":" is hist (the oldest
// first), whose getcmdcompltype() is compltype and where only the paths in
// exist exist.
func fakeHost(t *testing.T, hist []string, compltype string, exist ...string) *cmptest.Host {
	t.Helper()
	return &cmptest.Host{Funcs: map[string]func(...any) (any, error){
		"histnr": func(a ...any) (any, error) {
			if a[0] != ":" {
				return -1, nil
			}
			return len(hist), nil
		},
		"map": func(a ...any) (any, error) {
			if want := "histget(':', v:val)"; a[1] != want {
				t.Errorf("map expr = %q, want %q", a[1], want)
			}
			out := []any{}
			for _, i := range a[0].([]any) {
				out = append(out, hist[len(hist)+i.(int)])
			}
			return out, nil
		},
		"getcmdcompltype": func(...any) (any, error) { return compltype, nil },
		"filter": func(a ...any) (any, error) {
			out := []any{}
			for _, p := range a[0].([]any) {
				for _, e := range exist {
					if p == e {
						out = append(out, p)
					}
				}
			}
			return out, nil
		},
	}}
}

func TestHistory(t *testing.T) {
	ctx := context.Background()
	h := fakeHost(t, []string{"e a.txt", "", "set nu", "e missing", "echo 1"}, "")
	got, err := History(ctx, h, ":", 1000)
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"echo 1", "e missing", "set nu", "e a.txt"}; !reflect.DeepEqual(got, want) {
		t.Errorf("History = %q, want %q", got, want)
	}
	// The limit counts from the newest.
	got, _ = History(ctx, h, ":", 2)
	if want := []string{"echo 1", "e missing"}; !reflect.DeepEqual(got, want) {
		t.Errorf("History(limit 2) = %q, want %q", got, want)
	}
	// Another type has no history; no type (insert mode) has none either.
	for _, typ := range []string{"/", ""} {
		if got, _ := History(ctx, h, typ, 10); len(got) != 0 {
			t.Errorf("History(%q) = %q", typ, got)
		}
	}
	// Completing files: only the entries that exist.
	h = fakeHost(t, []string{"a.txt", "missing", "dir"}, "file", "a.txt", "dir")
	got, _ = History(ctx, h, ":", 10)
	if want := []string{"dir", "a.txt"}; !reflect.DeepEqual(got, want) {
		t.Errorf("History(file) = %q, want %q", got, want)
	}
}

func TestGather(t *testing.T) {
	h := fakeHost(t, []string{"edit foo.txt", "echo 1", "edit fo", "edit bar", "edit fizz\nbuzz", "edit "}, "")
	src := New(Params{})
	for _, tc := range []struct {
		input string
		start int
		words []string
	}{
		// No space yet: whole entries. Multi-line ones are left out
		// (ddc-source-cmdline_history leaves them out after a space only).
		{"ed", 0, []string{"edit ", "edit bar", "edit fo", "echo 1", "edit foo.txt"}},
		// After a space: the entries that start with the text before the
		// word, from the word on.
		{"edit fo", 5, []string{"bar", "fo", "foo.txt"}},
		{"echo ", 5, []string{"1"}},
	} {
		c := &cmp.Context{Mode: "c", CmdType: ":", Input: tc.input, Col: len(tc.input) + 1}
		g, err := src.Gather(context.Background(), h, c)
		if err != nil {
			t.Fatal(err)
		}
		var words []string
		for _, it := range g.Items {
			words = append(words, it.Word)
		}
		if g.Start != tc.start || !reflect.DeepEqual(words, tc.words) {
			t.Errorf("%q: start %d, words %q; want %d, %q", tc.input, g.Start, words, tc.start, tc.words)
		}
	}

	// Insert mode: nothing, and no call.
	h = &cmptest.Host{}
	g, err := src.Gather(context.Background(), h, &cmp.Context{Mode: "i", Input: "edit"})
	if err != nil || len(g.Items) != 0 || len(h.Calls()) != 0 {
		t.Errorf("insert mode: %+v, %v, calls %v", g, err, h.Calls())
	}
}

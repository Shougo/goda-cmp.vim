package sorterrank

import (
	"context"
	"reflect"
	"testing"

	"github.com/Shougo/goda-cmp.vim/cmp"
	"github.com/Shougo/goda-cmp.vim/cmp/cmptest"
)

func TestScore(t *testing.T) {
	for _, tc := range []struct {
		str, complete string
		cache         map[string]int
		lnum          int
		want          int
	}{
		{"", "", nil, 0, 200},
		{"abc", "a", nil, 0, 194},
		{"Abc", "a", nil, 0, 54},
		{"xAbc", "a", nil, 0, 12},
		{"xyz", "a", nil, 0, -6},
		{"abc", "a", map[string]int{"abc": 10}, 12, 342},
		{"abc", "a", map[string]int{"abc": 10}, 500, -146},
		{"abc", "", nil, 0, 194},
	} {
		if got := score(tc.str, tc.complete, tc.cache, tc.lnum); got != tc.want {
			t.Errorf("score(%q, %q) = %d, want %d", tc.str, tc.complete, got, tc.want)
		}
	}
}

// Words near the cursor rank higher; the buffer is read once per line.
func TestFilter(t *testing.T) {
	h := cmptest.Lines("abcdef far away", "", "", "abcxyz near")
	f := New()
	items := []cmp.Item{{Word: "abcdef"}, {Word: "abcxyz"}, {Word: "zzabc"}}
	c := &cmp.Context{Bufnr: 1, Lnum: 4}
	got, err := f.Filter(context.Background(), cmp.FilterInput{Context: c, Complete: "abc", Items: items, Host: h})
	if err != nil {
		t.Fatal(err)
	}
	var ws []string
	for _, it := range got {
		ws = append(ws, it.Word)
	}
	if want := []string{"abcxyz", "abcdef", "zzabc"}; !reflect.DeepEqual(ws, want) {
		t.Errorf("got %q, want %q", ws, want)
	}
	n := len(h.Calls())
	f.Filter(context.Background(), cmp.FilterInput{Context: c, Complete: "abcd", Items: items, Host: h})
	if len(h.Calls()) != n {
		t.Errorf("the buffer was read again for the same line")
	}
}

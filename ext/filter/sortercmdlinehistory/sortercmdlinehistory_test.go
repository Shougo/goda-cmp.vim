package sortercmdlinehistory

import (
	"context"
	"reflect"
	"testing"

	"github.com/Shougo/goda-cmp.vim/cmp"
	"github.com/Shougo/goda-cmp.vim/cmp/cmptest"
)

func TestScore(t *testing.T) {
	for _, tc := range []struct {
		def  int
		word string
		hist []string
		want int
	}{
		{100, "foo", []string{"bar", "baz", "qux"}, 100},
		{100, "ba", []string{"foo", "bar", "baz"}, 1},
		{100, "z", []string{"foo", "bar", "baz"}, 2},
		{0, "foo", []string{"foo", "foofoo"}, 0},
		{9, "o", []string{"foo", "boo", "baz"}, 0},
	} {
		if got := score(tc.def, tc.word, tc.hist); got != tc.want {
			t.Errorf("score(%d, %q, %q) = %d, want %d", tc.def, tc.word, tc.hist, got, tc.want)
		}
	}
}

func TestFilter(t *testing.T) {
	// The history, the newest last: "set number" is newer than "split".
	hist := []string{"edit x", "split", "set number"}
	h := &cmptest.Host{Funcs: map[string]func(...any) (any, error){
		"histnr": func(...any) (any, error) { return len(hist), nil },
		"map": func(a ...any) (any, error) {
			out := []any{}
			for _, i := range a[0].([]any) {
				out = append(out, hist[len(hist)+i.(int)])
			}
			return out, nil
		},
		"getcmdcompltype": func(...any) (any, error) { return "command", nil },
	}}
	items := []cmp.Item{{Word: "sort"}, {Word: "split"}, {Word: "set"}, {Word: "sleep"}, {Word: "edit"}}
	words := func(items []cmp.Item) []string {
		var out []string
		for _, it := range items {
			out = append(out, it.Word)
		}
		return out
	}
	f := New(Params{})
	c := &cmp.Context{Mode: "c", CmdType: ":"}
	out, err := f.Filter(context.Background(), cmp.FilterInput{Context: c, Items: items, Host: h})
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"set", "split", "edit", "sort", "sleep"}; !reflect.DeepEqual(words(out), want) {
		t.Errorf("sorted %q, want %q", words(out), want)
	}
	if want := []string{"sort", "split", "set", "sleep", "edit"}; !reflect.DeepEqual(words(items), want) {
		t.Errorf("the input changed: %q", words(items))
	}

	// Insert mode, or no editor: the order stays.
	for _, in := range []cmp.FilterInput{
		{Context: &cmp.Context{Mode: "i"}, Items: items, Host: h},
		{Context: c, Items: items},
	} {
		out, err := f.Filter(context.Background(), in)
		if err != nil || !reflect.DeepEqual(words(out), words(items)) {
			t.Errorf("%+v: %q, %v", in.Context, words(out), err)
		}
	}
}

package matcherprefix

import (
	"context"
	"reflect"
	"testing"

	"github.com/Shougo/goda-cmp.vim/cmp"
)

func words(items []cmp.Item) []string {
	var out []string
	for _, it := range items {
		out = append(out, it.Word)
	}
	return out
}

func TestFilter(t *testing.T) {
	items := []cmp.Item{{Word: "-option"}, {Word: "abc"}, {Word: "ax"}, {Word: "bc"}, {Word: "Abc"}}
	for _, tc := range []struct {
		n    *int
		want []string
	}{
		{nil, []string{"-option", "abc", "ax"}}, // default 1
		{new(2), []string{"-option", "abc"}},
		{new(0), []string{"-option", "abc", "ax", "bc", "Abc"}},
		{new(-3), []string{"-option", "abc", "ax", "bc", "Abc"}},
	} {
		got, _ := New(Params{PrefixLength: tc.n}).Filter(context.Background(), cmp.FilterInput{Complete: "ab", Items: items})
		if !reflect.DeepEqual(words(got), tc.want) {
			t.Errorf("prefixLength %v: %q, want %q", tc.n, words(got), tc.want)
		}
	}
	f, err := New(Params{}).(cmp.Configurable).WithParams(cmp.Params{"prefixLength": 2})
	if err != nil {
		t.Fatal(err)
	}
	got, _ := f.(cmp.Filter).Filter(context.Background(), cmp.FilterInput{Complete: "ab", Items: items})
	if !reflect.DeepEqual(words(got), []string{"-option", "abc"}) {
		t.Errorf("filterParams: %q", words(got))
	}
}

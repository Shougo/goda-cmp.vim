package matcherhead_test

import (
	"context"
	"reflect"
	"testing"

	"github.com/Shougo/goda-cmp.vim/cmp"
	"github.com/Shougo/goda-cmp.vim/ext/filter/matcherhead"
)

func TestMatcherHeadCase(t *testing.T) {
	f := matcherhead.New(matcherhead.Params{})
	items := []cmp.Item{{Word: "Foo"}, {Word: "foo"}, {Word: "bar"}}
	run := func(f cmp.Filter, typed string) []string {
		out, _ := f.Filter(context.Background(), cmp.FilterInput{Complete: typed, Items: items})
		var w []string
		for _, i := range out {
			w = append(w, i.Word)
		}
		return w
	}
	if got := run(f, "fo"); !reflect.DeepEqual(got, []string{"Foo", "foo"}) {
		t.Errorf("ignore case: %q", got)
	}
	if got := run(f, "Fo"); !reflect.DeepEqual(got, []string{"Foo"}) {
		t.Errorf("smartcase: %q", got)
	}
	if got := run(matcherhead.New(matcherhead.Params{CaseSensitive: true}), "fo"); !reflect.DeepEqual(got, []string{"foo"}) {
		t.Errorf("case sensitive: %q", got)
	}
}

package sorterhead

import (
	"context"
	"reflect"
	"testing"

	"github.com/Shougo/goda-cmp.vim/cmp"
)

func TestScore(t *testing.T) {
	for _, tc := range []struct {
		input, str string
		words      []string
		self       int
		want       float64
	}{
		{"", "a", []string{"a", "b"}, 0, 0},
		{"", "a", []string{"a", "b", "ab"}, 0, 0},
		{"", "a", []string{"a", "b", "a"}, 0, 0},
		{"", "a", []string{"a", "b", "a"}, 2, 2},
	} {
		if got := score(tc.input, tc.str, tc.words, tc.self); got != tc.want {
			t.Errorf("score(%q, %q, %q, %d) = %v, want %v", tc.input, tc.str, tc.words, tc.self, got, tc.want)
		}
	}
}

func TestFilter(t *testing.T) {
	var items []cmp.Item
	for _, w := range []string{"foobar_long", "x", "foo", "foobaz_long"} {
		items = append(items, cmp.Item{Word: w})
	}
	got, _ := New().Filter(context.Background(), cmp.FilterInput{Complete: "f", Items: items})
	var ws []string
	for _, it := range got {
		ws = append(ws, it.Word)
	}
	// "foo" heads the long ones: it moves up to the first, ahead of "x".
	if want := []string{"foobar_long", "foo", "x", "foobaz_long"}; !reflect.DeepEqual(ws, want) {
		t.Errorf("got %q, want %q", ws, want)
	}
}

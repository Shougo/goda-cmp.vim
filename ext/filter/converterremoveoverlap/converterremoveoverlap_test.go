package converterremoveoverlap

import (
	"context"
	"reflect"
	"testing"

	"github.com/Shougo/goda-cmp.vim/cmp"
	"github.com/Shougo/goda-cmp.vim/cmp/cmptest"
)

func TestOverlapLength(t *testing.T) {
	for _, tc := range []struct {
		left string
		next []string
		want int
	}{
		{"date", []string{"te"}, 2},
		{"foobar", []string{"bar"}, 3},
		{"foo", []string{"bar"}, 0},
		{"foobar baz", []string{"bar", " ", "baz"}, 7},
		{"foobar", []string{"bar", " ", "baz"}, 3},
		{"hello", []string{"bar", " ", "baz"}, 0},
		{"foobar", nil, 0},
		{"", []string{"bar"}, 0},
	} {
		if got := overlapLength(tc.left, tc.next); got != tc.want {
			t.Errorf("overlapLength(%q, %q) = %d, want %d", tc.left, tc.next, got, tc.want)
		}
	}
}

func TestTokens(t *testing.T) {
	if got, want := tokens("bar(1, x2)123"), []string{"bar", "(", "1", ",", " ", "x2", ")", "123"}; !reflect.DeepEqual(got, want) {
		t.Errorf("tokens = %q, want %q", got, want)
	}
}

func TestFilter(t *testing.T) {
	h := &cmptest.Host{}
	c := &cmp.Context{Input: "foo", NextInput: "bar"}
	got, _ := New().Filter(context.Background(), cmp.FilterInput{Context: c, Host: h,
		Items: []cmp.Item{{Word: "foobar"}, {Word: "food"}, {Word: "foo_bar", Abbr: "x"}}})
	want := []cmp.Item{{Word: "foo", Abbr: "foobar"}, {Word: "food"}, {Word: "foo_", Abbr: "x"}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %+v, want %+v", got, want)
	}
}

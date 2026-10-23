package matcherlength

import (
	"context"
	"reflect"
	"testing"

	"github.com/Shougo/goda-cmp.vim/cmp"
)

func TestFilter(t *testing.T) {
	got, _ := New().Filter(context.Background(), cmp.FilterInput{Complete: "あい", Items: []cmp.Item{{Word: "あい"}, {Word: "あいう"}, {Word: "a"}}})
	if len(got) != 1 || got[0].Word != "あいう" {
		t.Errorf("got %+v", got)
	}
	got, _ = New().Filter(context.Background(), cmp.FilterInput{Items: []cmp.Item{{Word: "x"}}})
	if !reflect.DeepEqual(got, []cmp.Item{{Word: "x"}}) {
		t.Errorf("empty input: %+v", got)
	}
}

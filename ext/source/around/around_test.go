package around_test

import (
	"context"
	"slices"
	"testing"

	"github.com/Shougo/goda-cmp.vim/cmp"
	"github.com/Shougo/goda-cmp.vim/cmp/cmptest"
	"github.com/Shougo/goda-cmp.vim/ext/source/around"
)

// The words of the lines around the cursor, the nearest lines first, never
// what is typed.
func TestGather(t *testing.T) {
	h := cmptest.Lines("func fooBarBaz() {", "\tfoo := fooBar + other", "\tfo")
	g, err := around.New(around.Params{}).Gather(context.Background(), h,
		&cmp.Context{Bufnr: 1, Lnum: 3, Input: "\tfo"})
	if err != nil {
		t.Fatal(err)
	}
	var words []string
	for _, it := range g.Items {
		words = append(words, it.Word)
	}
	if g.Start != 1 || slices.Contains(words, "fo") {
		t.Errorf("start %d words %q", g.Start, words)
	}
	if i, j := slices.Index(words, "foo"), slices.Index(words, "fooBarBaz"); i < 0 || j < 0 || i > j {
		t.Errorf("the words of the line above come first: %q", words)
	}
}

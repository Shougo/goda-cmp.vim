package sortershellhistory

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/Shougo/goda-cmp.vim/cmp"
)

func TestScore(t *testing.T) {
	if got := score(100, "foo", []string{"bar", "baz", "qux"}); got != 100 {
		t.Errorf("no match: %d", got)
	}
	if got := score(100, "ba", []string{"foo", "bar", "baz"}); got != 1 {
		t.Errorf("first match: %d", got)
	}
	if got := score(9, "o", []string{"foo", "boo", "baz"}); got != 0 {
		t.Errorf("several: %d", got)
	}
}

func TestFilter(t *testing.T) {
	path := filepath.Join(t.TempDir(), "hist")
	// zsh's extended format and plain lines; the oldest first.
	if err := os.WriteFile(path, []byte(": 1:0;git status\nmake test\n: 2:0;git log\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	items := []cmp.Item{{Word: "ls"}, {Word: "log"}, {Word: "make"}}
	got, _ := New(Params{Paths: []string{path}}).Filter(context.Background(), cmp.FilterInput{Items: items})
	var ws []string
	for _, it := range got {
		ws = append(ws, it.Word)
	}
	// The newest first: "git log" is the last line.
	if want := []string{"log", "make", "ls"}; !reflect.DeepEqual(ws, want) {
		t.Errorf("got %q, want %q", ws, want)
	}
}

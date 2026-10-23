package shellhistory

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/Shougo/goda-cmp.vim/cmp"
	"github.com/Shougo/goda-cmp.vim/cmp/cmptest"
)

func TestHistory(t *testing.T) {
	path := filepath.Join(t.TempDir(), "hist")
	if err := os.WriteFile(path, []byte(": 1:0;git status\nmake\n\n: 2:0;git log\nmake\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got, want := History(path, 10), []string{"make", "git log", "git status"}; !reflect.DeepEqual(got, want) {
		t.Errorf("History = %q, want %q", got, want)
	}
	if got, want := History(path, 2), []string{"make", "git log"}; !reflect.DeepEqual(got, want) {
		t.Errorf("History(2) = %q, want %q", got, want)
	}
	if got := History(filepath.Join(t.TempDir(), "none"), 10); len(got) != 0 {
		t.Errorf("no file: %q", got)
	}
}

func TestGather(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HISTDIR", dir)
	if err := os.WriteFile(filepath.Join(dir, "hist"), []byte("git status\ngit stash\nls -l\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	src := New(Params{Paths: []string{"$HISTDIR/hist"}})
	h := &cmptest.Host{Funcs: map[string]func(...any) (any, error){}}
	// Eval fails in cmptest for this expression: the input is Context.Input.
	g, err := src.Gather(context.Background(), h, &cmp.Context{Mode: "t", Input: "git st"})
	if err != nil {
		t.Fatal(err)
	}
	var ws []string
	for _, it := range g.Items {
		ws = append(ws, it.Word)
	}
	if g.Start != 4 || !reflect.DeepEqual(ws, []string{"stash", "status"}) {
		t.Errorf("start %d words %q", g.Start, ws)
	}
}

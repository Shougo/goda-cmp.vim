package lsp

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/Shougo/goda-cmp.vim/cmp"
	"github.com/Shougo/goda-cmp.vim/cmp/cmptest"
	"github.com/Shougo/goda.vim/sdk"
)

// goplsParams is the configuration a user would write for gopls; the tests
// run gopls offline.
func goplsParams(t *testing.T) Params {
	t.Helper()
	if testing.Short() {
		t.Skip("starts gopls")
	}
	bin := os.Getenv("GODA_GOPLS")
	if bin == "" {
		var err error
		if bin, err = exec.LookPath("gopls"); err != nil {
			t.Skip("gopls is not installed (or set GODA_GOPLS)")
		}
	}
	return Params{
		Name:        "gopls",
		Command:     []string{bin},
		Env:         []string{"GOPROXY=off", "GOFLAGS=-mod=mod", "GOTOOLCHAIN=local"},
		Filetypes:   []string{"go"},
		RootMarkers: []string{"go.mod"},
		Settings:    map[string]any{"gopls": map[string]any{"usePlaceholders": false}},
		InitTimeout: 60 * time.Second,
	}
}

func writeModule(t *testing.T, src string) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module example.com/m\n\ngo 1.24\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "main.go")
	if err := os.WriteFile(path, []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

// gatherRetry asks until gopls answers: the first requests may come while it
// is still loading the module.
func gatherRetry(t *testing.T, s *Source, h sdk.Host, c *cmp.Context, want string) cmp.Gathered {
	t.Helper()
	deadline := time.Now().Add(60 * time.Second)
	for {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		g, err := cmp.Collect(ctx, h, c, s)
		cancel()
		if err == nil && slices.Contains(words(g), want) {
			return g
		}
		if time.Now().After(deadline) {
			t.Fatalf("no %q from gopls: words %q, err %v", want, words(g), err)
		}
		time.Sleep(200 * time.Millisecond)
	}
}

func TestGopls(t *testing.T) {
	p := goplsParams(t)
	src := "package main\n\nimport \"fmt\"\n\nfunc main() {\n\tfmt.Pri\n}\n"
	path := writeModule(t, src)
	s := New(p)
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		_ = s.Close(ctx)
	}()
	if err := s.Init(context.Background(), nil); err != nil {
		t.Fatal(err)
	}
	ed := cmptest.NewEditor(path, strings.Split(strings.TrimSuffix(src, "\n"), "\n")...)
	vim := ed.Host()

	// Member completion: the word starts after the dot.
	c := &cmp.Context{Bufnr: 1, Tick: 1, Lnum: 6, Input: "\tfmt.Pri", Filetype: "go"}
	g := gatherRetry(t, s, vim, c, "Println")
	if g.Start != len("\tfmt.") {
		t.Errorf("Start = %d, want %d", g.Start, len("\tfmt."))
	}
	var println cmp.Item
	for _, it := range g.Items {
		if it.Word == "Println" {
			println = it
		}
	}
	t.Logf("Println: kind %q abbr %q info %q sort %q", println.Kind, println.Abbr, println.Info, println.SortText)
	if println.Kind != "Function" {
		t.Errorf("kind = %q", println.Kind)
	}
	it, err := s.Resolve(context.Background(), vim, cmp.DoneArgs{Context: c, Item: println})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(it.Info, "Println") {
		t.Errorf("resolved info = %q", it.Info)
	}

	// The trigger character alone: "fmt." offers members.
	lines := ed.Lines(1)
	lines[5] = "\tfmt."
	ed.SetLines(1, lines...)
	c = &cmp.Context{Bufnr: 1, Tick: 2, Lnum: 6, Input: "\tfmt.", Filetype: "go"}
	gatherRetry(t, s, vim, c, "Sprintf")
}

// An unimported package: accepting the item adds the import
// (additionalTextEdits, possibly only after resolve).
func TestGoplsImport(t *testing.T) {
	p := goplsParams(t)
	src := "package main\n\nfunc main() {\n\t_ = strings.ToU\n}\n"
	path := writeModule(t, src)
	s := New(p)
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		_ = s.Close(ctx)
	}()
	ed := cmptest.NewEditor(path, strings.Split(strings.TrimSuffix(src, "\n"), "\n")...)
	vim := ed.Host()
	c := &cmp.Context{Bufnr: 1, Tick: 1, Lnum: 4, Input: "\t_ = strings.ToU", Filetype: "go"}
	g := gatherRetry(t, s, vim, c, "ToUpper")
	var item cmp.Item
	for _, it := range g.Items {
		if it.Word == "ToUpper" {
			item = it
		}
	}
	// What Vim does on accept: the word replaces the typed text.
	lines := ed.Lines(1)
	lines[3] = "\t_ = strings.ToUpper"
	ed.SetLines(1, lines...)
	ed.SetCursor(4, len(lines[3])+1)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := s.OnCompleteDone(ctx, vim, cmp.DoneArgs{Context: c, Item: item}); err != nil {
		t.Fatal(err)
	}
	got := strings.Join(ed.Lines(1), "\n")
	t.Logf("after accept:\n%s", got)
	if !strings.Contains(got, `import "strings"`) || !strings.Contains(got, "\t_ = strings.ToUpper") {
		t.Errorf("import not added:\n%s", got)
	}
	// The cursor follows its line.
	if ln, col := ed.Cursor(); ed.Lines(1)[ln-1] != "\t_ = strings.ToUpper" || col != len("\t_ = strings.ToUpper")+1 {
		t.Errorf("cursor %d, %d is on %q", ln, col, ed.Lines(1)[ln-1])
	}
}

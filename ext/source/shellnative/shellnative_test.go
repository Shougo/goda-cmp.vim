package shellnative

import (
	"context"
	"os"
	"os/exec"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/Shougo/goda-cmp.vim/cmp"
	"github.com/Shougo/goda-cmp.vim/cmp/cmptest"
)

func TestCmdPrefix(t *testing.T) {
	for in, want := range map[string]string{
		"!ls -":            "!",
		"silent !git st":   "silent !",
		"%!sort":           "%!",
		"terminal ++close": "terminal ++close",
		"echo 1":           "",
		"'<,'>!sort":       "'<,'>!",
	} {
		if got := cmdPrefix.FindString(in); got != want {
			t.Errorf("cmdPrefix(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestUnsupportedShell(t *testing.T) {
	if err := New(Params{Shell: "csh"}).(source).Init(context.Background(), nil); err == nil {
		t.Error("csh: no error")
	}
}

// With zsh installed: a real completion of a command name.
func TestZsh(t *testing.T) {
	if _, err := exec.LookPath("zsh"); err != nil {
		t.Skip("zsh is not installed")
	}
	src := New(Params{Shell: "zsh"}).(source)
	h := &cmptest.Host{Funcs: map[string]func(...any) (any, error){
		"getcwd":  func(...any) (any, error) { return t.TempDir(), nil },
		"environ": func(...any) (any, error) { return map[string]any{"PATH": os.Getenv("PATH"), "HOME": t.TempDir()}, nil },
	}}
	ctx := context.Background()
	if err := src.Init(ctx, h); err != nil {
		t.Fatal(err)
	}
	defer src.Close(ctx)
	g, err := src.Gather(ctx, h, &cmp.Context{Mode: "t", Input: "ech"})
	if err != nil {
		t.Fatal(err)
	}
	var ws []string
	for _, it := range g.Items {
		ws = append(ws, it.Word)
	}
	if g.Start != 0 || !reflect.DeepEqual(ws[:min(1, len(ws))], []string{"echo"}) {
		t.Errorf("start %d words %q", g.Start, ws)
	}
}

// A request that gives up does not get its answer read by the next one.
func TestStaleAnswer(t *testing.T) {
	if _, err := exec.LookPath("zsh"); err != nil {
		t.Skip("zsh is not installed")
	}
	src := New(Params{Shell: "zsh"}).(source)
	h := &cmptest.Host{Funcs: map[string]func(...any) (any, error){
		"getcwd":  func(...any) (any, error) { return t.TempDir(), nil },
		"environ": func(...any) (any, error) { return map[string]any{"PATH": os.Getenv("PATH"), "HOME": t.TempDir()}, nil },
	}}
	ctx := context.Background()
	if err := src.Init(ctx, h); err != nil {
		t.Fatal(err)
	}
	defer src.Close(ctx)
	gone, cancel := context.WithCancel(ctx)
	cancel()
	if _, err := src.complete(gone, "ech", ""); err == nil {
		t.Fatal("a cancelled request answered")
	}
	got, err := src.complete(ctx, "printf", "")
	if err != nil {
		t.Fatal(err)
	}
	// The lines of the answer (empty ones are dropped by Gather).
	for _, l := range got {
		if l != "" && !strings.HasPrefix(l, "printf") {
			t.Errorf("the answer for printf has %q: %q", l, got)
		}
	}
	if !slices.Contains(got, "printf") {
		t.Errorf("the answer for printf: %q", got)
	}
}

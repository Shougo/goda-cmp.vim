package cmp_test

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

type dump struct {
	Visible bool     `json:"visible"`
	Words   []string `json:"words"`
	Line    string   `json:"line"`
	Error   string   `json:"error"`
	Buffer  any      `json:"buffer"`
}

// TestRealVim types into an actual Vim (in a pseudo terminal) with a binary
// built from examples/config and checks what the popup shows. GODA_CMP_PUM
// may name a checkout of pum.vim to run the same scenario with the pum UI.
func TestRealVim(t *testing.T) {
	if testing.Short() {
		t.Skip("builds a binary and starts Vim")
	}
	vimBin, err := exec.LookPath("vim")
	if err != nil {
		t.Skip("vim is not installed")
	}
	if _, err := exec.LookPath("python3"); err != nil {
		t.Skip("python3 is needed to drive the pty")
	}
	ver, _ := exec.Command(vimBin, "--version").Output()
	if !strings.Contains(string(ver), "+job") || !strings.Contains(string(ver), "+channel") || !strings.Contains(string(ver), "+timers") {
		t.Skip("this vim lacks +job/+channel/+timers")
	}

	repo, _ := filepath.Abs("..")
	godaRoot, _ := filepath.Abs("../../goda.vim")
	if _, err := os.Stat(godaRoot); err != nil {
		t.Skip("goda.vim is not next to goda-cmp.vim")
	}
	dir := t.TempDir()
	bin := filepath.Join(dir, "goda-cmp")
	// examples/config has no replace directives (it is written the way a
	// user's module is), so build a copy that resolves both modules to the
	// local checkouts.
	cfgDir := filepath.Join(dir, "config")
	if err := os.Mkdir(cfgDir, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, f := range []string{"main.go", "config.go"} {
		b, err := os.ReadFile(filepath.Join(repo, "examples", "config", f))
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(cfgDir, f), b, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	gomod := "module goda-cmp-config\n\ngo 1.24\n\n" +
		"require (\n\tgithub.com/Shougo/goda-cmp.vim v0.0.0\n\tgithub.com/Shougo/goda.vim v0.0.0\n)\n\n" +
		"replace github.com/Shougo/goda-cmp.vim => " + repo + "\n" +
		"replace github.com/Shougo/goda.vim => " + godaRoot + "\n"
	if err := os.WriteFile(filepath.Join(cfgDir, "go.mod"), []byte(gomod), 0o644); err != nil {
		t.Fatal(err)
	}
	build := exec.Command("go", "build", "-o", bin, ".")
	build.Dir = cfgDir
	build.Env = append(os.Environ(), "CGO_ENABLED=0")
	if b, err := build.CombinedOutput(); err != nil {
		t.Fatalf("building examples/config: %v\n%s", err, b)
	}

	text := filepath.Join(dir, "text.txt")
	if err := os.WriteFile(text, []byte("foobar\nfoo_baz\nfoobarbaz\nzzz\n\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	run := func(t *testing.T, pumDir, completeopt string) []dump {
		out := filepath.Join(dir, "out-"+strings.ReplaceAll(t.Name(), "/", "-")+".jsonl")
		steps := `[
		  ["Go", 0.3],
		  ["fo", 1.0], ["\u001d", 0.3],
		  ["o", 1.0],  ["\u001d", 0.3],
		  ["b", 1.0],  ["\u001d", 0.3],
		  ["\u001b", 0.5],
		  ["o", 0.3], ["qq", 1.0], ["\u001d", 0.3],
		  ["\u001b", 0.3],
		  ["o", 0.3], ["\u0007", 0.3], ["fo", 1.0], ["\u001d", 0.3],
		  ["\u0014", 0.5], ["o", 1.0], ["\u001d", 0.3],
		  ["\u0019", 0.3],
		  ["\u001b", 0.3]
		]`
		cmd := exec.Command("python3", "-I", "testdata/drive.py", vimBin, "testdata/vimrc.vim", text, steps)
		cmd.Env = append(os.Environ(),
			"GODA_RTP="+godaRoot, "GODA_CMP_RTP="+repo, "GODA_CMP_BIN="+bin, "GODA_OUT="+out,
			"PUM_RTP="+pumDir, "GODA_COMPLETEOPT="+completeopt)
		if b, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("driver: %v\n%s", err, b)
		}
		raw, err := os.ReadFile(out)
		if err != nil {
			t.Fatalf("Vim recorded nothing: %v", err)
		}
		var ds []dump
		for _, l := range strings.Split(strings.TrimSpace(string(raw)), "\n") {
			var d dump
			if err := json.Unmarshal([]byte(l), &d); err != nil {
				t.Fatalf("bad record %q: %v", l, err)
			}
			ds = append(ds, d)
		}
		t.Logf("records: %+v", ds)
		return ds
	}

	check := func(t *testing.T, ds []dump) {
		want := []dump{
			{true, []string{"foobar", "foo_baz", "foobarbaz"}, "fo", "", nil},
			{true, []string{"foobar", "foo_baz", "foobarbaz"}, "foo", "", nil},
			{true, []string{"foobar", "foobarbaz"}, "foob", "", nil},
			{false, nil, "qq", "", nil}, // nothing in the buffer starts with qq
			// Sources switched off for the buffer by a run-time patch.
			{false, nil, "fo", "", nil},
			// Put back: completion works again.
			{true, []string{"foob", "foobar", "foo_baz", "foobarbaz"}, "foo", "", nil},
		}
		if len(ds) != len(want)+1 {
			t.Fatalf("got %d records, want %d", len(ds), len(want)+1)
		}
		for i, w := range want {
			g := ds[i]
			if g.Visible != w.Visible || (len(g.Words)+len(w.Words) > 0 && !reflect.DeepEqual(g.Words, w.Words)) || g.Line != w.Line {
				t.Errorf("record %d:\n got %+v\nwant %+v", i, g, w)
			}
		}
		// A patch naming an unknown source is refused, and changes nothing.
		bad := ds[len(want)]
		if !strings.Contains(bad.Error, "unknown source") {
			t.Errorf("invalid patch: error = %q, want one about an unknown source", bad.Error)
		}
		if m, ok := bad.Buffer.(map[string]any); !ok || len(m) != 0 {
			t.Errorf("invalid patch changed the buffer settings: %v", bad.Buffer)
		}
	}

	t.Run("native", func(t *testing.T) { check(t, run(t, "", "")) })
	// The popup must never change the text, whatever the user's 'completeopt'
	// is: without noinsert the first candidate would be inserted, and with
	// longest the common prefix.
	t.Run("native-with-default-completeopt", func(t *testing.T) { check(t, run(t, "", "menu,preview,longest")) })
	// The stand-in checks that the pum UI calls pum#open(startcol, items)
	// with the right items; it cannot check how pum.vim draws them.
	fake, _ := filepath.Abs("testdata/fakepum")
	t.Run("pum-with-stand-in", func(t *testing.T) { check(t, run(t, fake, "")) })
	if pum := os.Getenv("GODA_CMP_PUM"); pum != "" {
		t.Run("pum", func(t *testing.T) { check(t, run(t, pum, "")) })
	}
}

// buildConfig builds a configuration module (main.go files in srcDir) against
// the local checkouts and returns the binary.
func buildConfig(t *testing.T, srcDir string) (bin, repo, godaRoot string) {
	t.Helper()
	repo, _ = filepath.Abs("..")
	godaRoot, _ = filepath.Abs("../../goda.vim")
	if _, err := os.Stat(godaRoot); err != nil {
		t.Skip("goda.vim is not next to goda-cmp.vim")
	}
	dir := t.TempDir()
	cfgDir := filepath.Join(dir, "config")
	if err := os.Mkdir(cfgDir, 0o755); err != nil {
		t.Fatal(err)
	}
	ents, err := os.ReadDir(srcDir)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range ents {
		b, err := os.ReadFile(filepath.Join(srcDir, e.Name()))
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(cfgDir, e.Name()), b, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	gomod := "module goda-cmp-config\n\ngo 1.24\n\n" +
		"require (\n\tgithub.com/Shougo/goda-cmp.vim v0.0.0\n\tgithub.com/Shougo/goda.vim v0.0.0\n)\n\n" +
		"replace github.com/Shougo/goda-cmp.vim => " + repo + "\n" +
		"replace github.com/Shougo/goda.vim => " + godaRoot + "\n"
	if err := os.WriteFile(filepath.Join(cfgDir, "go.mod"), []byte(gomod), 0o644); err != nil {
		t.Fatal(err)
	}
	bin = filepath.Join(dir, "goda-cmp")
	build := exec.Command("go", "build", "-o", bin, ".")
	build.Dir = cfgDir
	build.Env = append(os.Environ(), "CGO_ENABLED=0")
	if b, err := build.CombinedOutput(); err != nil {
		t.Fatalf("building %s: %v\n%s", srcDir, err, b)
	}
	return bin, repo, godaRoot
}

// TestRealVimStream: partial results arrive in a real Vim, a popup the user is
// choosing from is left alone by later updates, and accepting an item reaches
// the source's OnCompleteDone with the item's UserData.
func TestRealVimStream(t *testing.T) {
	if testing.Short() {
		t.Skip("builds a binary and starts Vim")
	}
	vimBin, err := exec.LookPath("vim")
	if err != nil {
		t.Skip("vim is not installed")
	}
	if _, err := exec.LookPath("python3"); err != nil {
		t.Skip("python3 is needed to drive the pty")
	}
	bin, repo, godaRoot := buildConfig(t, "testdata/streamcfg")
	dir := t.TempDir()
	text := filepath.Join(dir, "text.txt")
	if err := os.WriteFile(text, []byte("\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(dir, "out.jsonl")
	done := filepath.Join(dir, "done.txt")
	// 1. type "al": the fast answer is shown, then the complete one.
	// 2. new line, type "al", select the first item while only the fast answer
	//    is there, wait for the late answer (must not drop the selection),
	//    accept with Ctrl-Y.
	steps := `[
	  ["Oal", 0.5], ["\u001d", 0.2],
	  ["", 1.2], ["\u001d", 0.2],
	  ["\u001b", 0.3],
	  ["o", 0.2], ["al", 0.5], ["\u000e", 0.3],
	  ["", 1.2], ["\u001d", 0.2],
	  ["\u0019", 0.5], ["\u001d", 0.2],
	  ["\u001b", 0.3]
	]`
	cmd := exec.Command("python3", "-I", "testdata/drive.py", vimBin, "testdata/vimrc.vim", text, steps)
	cmd.Env = append(os.Environ(),
		"GODA_RTP="+godaRoot, "GODA_CMP_RTP="+repo, "GODA_CMP_BIN="+bin, "GODA_OUT="+out,
		"GODA_DONE_FILE="+done, "GODA_NO_PATCH_MAPS=1", "PUM_RTP=")
	if b, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("driver: %v\n%s", err, b)
	}
	raw, err := os.ReadFile(out)
	if err != nil {
		t.Fatalf("Vim recorded nothing: %v", err)
	}
	var ds []dump
	for _, l := range strings.Split(strings.TrimSpace(string(raw)), "\n") {
		var d dump
		if err := json.Unmarshal([]byte(l), &d); err != nil {
			t.Fatalf("bad record %q: %v", l, err)
		}
		ds = append(ds, d)
	}
	t.Logf("records: %+v", ds)
	if len(ds) != 4 {
		t.Fatalf("got %d records, want 4", len(ds))
	}
	// Fast answer first (items are listed in popup order), complete one later.
	if !ds[0].Visible || !reflect.DeepEqual(ds[0].Words, []string{"alpha_fast"}) {
		t.Errorf("partial update: %+v", ds[0])
	}
	if !ds[1].Visible || !reflect.DeepEqual(ds[1].Words, []string{"alpha_fast", "alpha_late"}) {
		t.Errorf("final update: %+v", ds[1])
	}
	// Selecting inserted alpha_fast; the late answer did not take the popup
	// away from under the user (the native popup cannot be updated while an
	// item is selected).
	if ds[2].Line != "alpha_fast" || !ds[2].Visible || !reflect.DeepEqual(ds[2].Words, []string{"alpha_fast"}) {
		t.Errorf("after the late answer, the selection was lost: %+v", ds[2])
	}
	if ds[3].Line != "alpha_fast" || ds[3].Visible {
		t.Errorf("after Ctrl-Y: %+v", ds[3])
	}
	b, _ := os.ReadFile(done)
	if got := strings.TrimSpace(string(b)); got != "alpha_fast id-1" {
		t.Errorf("OnCompleteDone recorded %q, want %q", got, "alpha_fast id-1")
	}
}

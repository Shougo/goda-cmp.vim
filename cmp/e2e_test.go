package cmp_test

import (
	"encoding/json"
	"github.com/Shougo/goda-cmp.vim/internal/e2e"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"
)

type dump struct {
	Visible bool     `json:"visible"`
	Words   []string `json:"words"`
	Line    string   `json:"line"`
	Cmdline string   `json:"cmdline"`
	Left    bool     `json:"left"` // User GodaCmpCmdlineLeave
	Error   string   `json:"error"`
	Buffer  any      `json:"buffer"`
	Preview string   `json:"preview"` // the 'buftype' of pum.vim's preview
	Inline  bool     `json:"inline"`  // goda_cmp#ui#inline#visible()
	Current *struct {
		Mark    string `json:"mark"`
		Visible bool   `json:"visible"`
	} `json:"current"` // DumpCurrent()
}

// TestRealEditor types into an actual Vim (in a pseudo terminal) with a binary
// built from examples/config and checks what the popup shows. GODA_CMP_PUM
// may name a checkout of pum.vim to run the same scenario with the pum UI.
func TestRealEditor(t *testing.T) {
	if testing.Short() {
		t.Skip("builds a binary and starts an editor")
	}
	if _, err := exec.LookPath("python3"); err != nil {
		t.Skip("python3 is needed to drive the pty")
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
	gomod := "module goda-cmp-config\n\ngo 1.26\n\n" +
		"require (\n\tgithub.com/Shougo/goda-cmp.vim v0.0.0\n\tgithub.com/Shougo/goda.vim v0.0.0\n)\n\n" +
		"replace github.com/Shougo/goda-cmp.vim => " + repo + "\n" +
		"replace github.com/Shougo/goda.vim => " + godaRoot + "\n"
	if err := os.WriteFile(filepath.Join(cfgDir, "go.mod"), []byte(gomod), 0o644); err != nil {
		t.Fatal(err)
	}
	goBuild(t, cfgDir, bin)

	text := filepath.Join(dir, "text.txt")
	if err := os.WriteFile(text, []byte("foobar\nfoo_baz\nfoobarbaz\nzzz\n\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	e2e.Run(t, func(t *testing.T, ed e2e.Editor) {
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
			cmd := exec.Command("python3", "-I", "testdata/drive.py", ed.Bin, "testdata/vimrc.vim", text, steps)
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
			for l := range strings.SplitSeq(strings.TrimSpace(string(raw)), "\n") {
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
				{Visible: true, Words: []string{"foobar", "foo_baz", "foobarbaz"}, Line: "fo"},
				{Visible: true, Words: []string{"foobar", "foo_baz", "foobarbaz"}, Line: "foo"},
				{Visible: true, Words: []string{"foobar", "foobarbaz"}, Line: "foob"},
				{Visible: false, Words: nil, Line: "qq"}, // nothing in the buffer starts with qq
				// Sources switched off for the buffer by a run-time patch.
				{Visible: false, Words: nil, Line: "fo"},
				// Put back: completion works again.
				{Visible: true, Words: []string{"foob", "foobar", "foo_baz", "foobarbaz"}, Line: "foo"},
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
		if pum := os.Getenv("GODA_CMP_PUM"); pum != "" && supportsPum(ed) {
			t.Run("pum", func(t *testing.T) { check(t, run(t, pum, "")) })
		}
	})
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
	gomod := "module goda-cmp-config\n\ngo 1.26\n\n" +
		"require (\n\tgithub.com/Shougo/goda-cmp.vim v0.0.0\n\tgithub.com/Shougo/goda.vim v0.0.0\n)\n\n" +
		"replace github.com/Shougo/goda-cmp.vim => " + repo + "\n" +
		"replace github.com/Shougo/goda.vim => " + godaRoot + "\n"
	if err := os.WriteFile(filepath.Join(cfgDir, "go.mod"), []byte(gomod), 0o644); err != nil {
		t.Fatal(err)
	}
	bin = filepath.Join(dir, "goda-cmp")
	goBuild(t, cfgDir, bin)
	return bin, repo, godaRoot
}

// goBuild builds the configuration module in dir. The go.mod written by the
// tests requires both modules at v0.0.0, while goda-cmp.vim's own go.mod asks
// for a newer goda.vim; "go mod tidy" settles the requirements (offline: both
// are replaced by local directories) so that go build does not refuse.
func goBuild(t *testing.T, dir, bin string) {
	t.Helper()
	for _, args := range [][]string{{"mod", "tidy"}, {"build", "-o", bin, "."}} {
		cmd := exec.Command("go", args...)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(), "CGO_ENABLED=0")
		if b, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("go %s in %s: %v\n%s", strings.Join(args, " "), dir, err, b)
		}
	}
}

// TestRealStream: partial results arrive in a real Vim, a popup the user is
// choosing from is left alone by later updates, and accepting an item reaches
// the source's OnCompleteDone with the item's UserData.
func TestRealStream(t *testing.T) {
	if testing.Short() {
		t.Skip("builds a binary and starts an editor")
	}
	if _, err := exec.LookPath("python3"); err != nil {
		t.Skip("python3 is needed to drive the pty")
	}
	bin, repo, godaRoot := buildConfig(t, "testdata/streamcfg")
	e2e.Run(t, func(t *testing.T, ed e2e.Editor) {
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
		cmd := exec.Command("python3", "-I", "testdata/drive.py", ed.Bin, "testdata/vimrc.vim", text, steps)
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
		for l := range strings.SplitSeq(strings.TrimSpace(string(raw)), "\n") {
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
	})
}

// TestRealEvents: buffer events reach a source that asked for them, only
// for its filetype, in order, starting with a BufEnter for the buffer that
// was current when goda-cmp was enabled.
func TestRealEvents(t *testing.T) {
	if testing.Short() {
		t.Skip("builds a binary and starts an editor")
	}
	if _, err := exec.LookPath("python3"); err != nil {
		t.Skip("python3 is needed to drive the pty")
	}
	bin, repo, godaRoot := buildConfig(t, "testdata/eventcfg")
	e2e.Run(t, func(t *testing.T, ed e2e.Editor) {
		dir := t.TempDir()
		a := filepath.Join(dir, "a.txt")
		if err := os.WriteFile(a, []byte("a\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		events := filepath.Join(dir, "events.txt")
		// a.txt (current at start), c.md (not text), b.txt, then :bd of b.txt.
		stepList := [][]any{
			{":e " + filepath.Join(dir, "c.md") + "\r", 0.5},
			{":e " + filepath.Join(dir, "b.txt") + "\r", 0.5},
			{":bd\r", 1.0},
		}
		steps, _ := json.Marshal(stepList)
		cmd := exec.Command("python3", "-I", "testdata/drive.py", ed.Bin, "testdata/vimrc.vim", a, string(steps))
		cmd.Env = append(os.Environ(),
			"GODA_RTP="+godaRoot, "GODA_CMP_RTP="+repo, "GODA_CMP_BIN="+bin,
			"GODA_OUT="+filepath.Join(dir, "out.jsonl"), "GODA_EVENT_FILE="+events, "PUM_RTP=")
		if b, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("driver: %v\n%s", err, b)
		}
		b, err := os.ReadFile(events)
		if err != nil {
			t.Fatalf("no events recorded: %v", err)
		}
		want := "BufEnter text a.txt\nBufEnter text b.txt\nBufDelete text b.txt\n"
		if string(b) != want {
			t.Errorf("events:\n%s\nwant:\n%s", b, want)
		}
	})
}

// TestRealBadConfig: a binary built from an invalid configuration starts,
// and goda_cmp#enable() throws with the reason instead of a handshake error.
func TestRealBadConfig(t *testing.T) {
	if testing.Short() {
		t.Skip("builds a binary and starts an editor")
	}
	bin, repo, godaRoot := buildConfig(t, "testdata/badcfg")
	e2e.Run(t, func(t *testing.T, ed e2e.Editor) {
		dir := t.TempDir()
		out := filepath.Join(dir, "out.txt")
		script := filepath.Join(dir, "test.vim")
		vimscript := `set nocompatible
let &runtimepath = $GODA_RTP . ',' . $GODA_CMP_RTP . ',' . &runtimepath
let g:goda_cmp#binary = $GODA_CMP_BIN
try
  call goda_cmp#enable()
  let s:r = ['no error']
catch
  let s:r = [v:exception]
endtry
call add(s:r, 'running=' . goda#is_running('cmp'))
call writefile(s:r, $GODA_OUT)
qa!
`
		if err := os.WriteFile(script, []byte(vimscript), 0o644); err != nil {
			t.Fatal(err)
		}
		cmd := exec.Command(ed.Bin, ed.Headless(script)...)
		cmd.Env = append(os.Environ(), "GODA_RTP="+godaRoot, "GODA_CMP_RTP="+repo, "GODA_CMP_BIN="+bin, "GODA_OUT="+out)
		if b, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("vim: %v\n%s", err, b)
		}
		b, err := os.ReadFile(out)
		if err != nil {
			t.Fatal(err)
		}
		got := string(b)
		t.Logf("%s", got)
		if !strings.Contains(got, "fix the configuration") || !strings.Contains(got, `unknown filter "matcher_head"`) {
			t.Errorf("enable() did not report the configuration error:\n%s", got)
		}
		if !strings.Contains(got, "running=0") {
			t.Errorf("the binary was left running:\n%s", got)
		}
	})
}

// TestRealNoCmpPlugin: goda_cmp#enable() with a binary that has no "cmp"
// plugin stops that binary only, not the binaries of other plugins.
func TestRealNoCmpPlugin(t *testing.T) {
	if testing.Short() {
		t.Skip("builds a binary and starts an editor")
	}
	built, repo, godaRoot := buildConfig(t, "testdata/nocmp")
	e2e.Run(t, func(t *testing.T, ed e2e.Editor) {
		dir := t.TempDir()
		// Two copies: "wrong" is what goda-cmp is pointed at, "bystander" is
		// another plugin's binary that must keep running.
		data, err := os.ReadFile(built)
		if err != nil {
			t.Fatal(err)
		}
		wrong, bystander := filepath.Join(dir, "wrong"), filepath.Join(dir, "bystander")
		for _, p := range []string{wrong, bystander} {
			if err := os.WriteFile(p, data, 0o755); err != nil {
				t.Fatal(err)
			}
		}
		out := filepath.Join(dir, "out.txt")
		script := filepath.Join(dir, "test.vim")
		vimscript := `set nocompatible
let &runtimepath = $GODA_RTP . ',' . $GODA_CMP_RTP . ',' . &runtimepath
let g:goda_cmp#binary = $GODA_CMP_BIN
call goda#start($GODA_BYSTANDER)
try
  call goda_cmp#enable()
  let s:r = ['no error']
catch
  let s:r = [v:exception]
endtry
call add(s:r, 'wrong=' . goda#is_running('wrong'))
call add(s:r, 'bystander=' . goda#is_running('bystander'))
call writefile(s:r, $GODA_OUT)
call goda#stop()
qa!
`
		if err := os.WriteFile(script, []byte(vimscript), 0o644); err != nil {
			t.Fatal(err)
		}
		cmd := exec.Command(ed.Bin, ed.Headless(script)...)
		cmd.Env = append(os.Environ(), "GODA_RTP="+godaRoot, "GODA_CMP_RTP="+repo,
			"GODA_CMP_BIN="+wrong, "GODA_BYSTANDER="+bystander, "GODA_OUT="+out)
		if b, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("vim: %v\n%s", err, b)
		}
		b, err := os.ReadFile(out)
		if err != nil {
			t.Fatal(err)
		}
		got := string(b)
		t.Logf("%s", got)
		if !strings.Contains(got, `does not contain the "cmp" plugin`) {
			t.Errorf("enable() did not refuse the binary:\n%s", got)
		}
		if !strings.Contains(got, "wrong=0") || !strings.Contains(got, "bystander=1") {
			t.Errorf("the wrong binary must stop and the other one keep running:\n%s", got)
		}
	})
}

// TestRealCmdline: command-line completion in a real editor. ":" enables it
// for that command line (as in ddc.vim's documentation), the cmdline source
// offers what getcompletion() does, the pum UI shows it, and leaving the
// command line ends it: the next one ("/") is not completed. It runs with the
// pum.vim stand-in, and with pum.vim itself when GODA_CMP_PUM names a
// checkout and the editor is new enough for it.
func TestRealCmdline(t *testing.T) {
	if testing.Short() {
		t.Skip("builds a binary and starts an editor")
	}
	if _, err := exec.LookPath("python3"); err != nil {
		t.Skip("python3 is needed to drive the pty")
	}
	bin, repo, godaRoot := buildConfig(t, "testdata/cmdlinecfg")
	fake, _ := filepath.Abs("testdata/fakepum")
	e2e.Run(t, func(t *testing.T, ed e2e.Editor) {
		pums := map[string]string{"pum-stand-in": fake}
		if real := os.Getenv("GODA_CMP_PUM"); real != "" && supportsPum(ed) {
			pums["pum"] = real
		}
		for name, pumDir := range pums {
			t.Run(name, func(t *testing.T) {
				dir := t.TempDir()
				text := filepath.Join(dir, "text.txt")
				if err := os.WriteFile(text, []byte("x\n"), 0o644); err != nil {
					t.Fatal(err)
				}
				out := filepath.Join(dir, "out.jsonl")
				steps := `[
				  [":", 0.5], ["set filety", 1.5], ["\u001d", 0.3],
				  ["\u001b", 0.5],
				  ["/filety", 1.0], ["\u001d", 0.3],
				  ["\u001b", 0.3]
				]`
				real := name == "pum"
				if real {
					// Then select (Ctrl-N inserts the item: no new
					// completion for that) and accept with Ctrl-Y.
					steps = `[
					  [":", 0.5], ["set filety", 1.5], ["\u001d", 0.3],
					  ["\u001b", 0.5],
					  ["/filety", 1.0], ["\u001d", 0.3],
					  ["\u001b", 0.3],
					  [":", 0.5], ["set filety", 1.5], ["\u000e", 0.8], ["\u001d", 0.3],
					  ["\u0019", 0.8], ["\u001d", 0.3],
					  ["\u001b", 0.3],
					  [":", 0.5], ["let g:hist_marker = 1\r", 0.5],
					  [":", 0.5], ["let g:hist_m", 1.5], ["\u001d", 0.3],
					  ["\u001b", 0.3]
					]`
				}
				cmd := exec.Command("python3", "-I", "testdata/drive.py", ed.Bin, "testdata/vimrc.vim", text, steps)
				cmd.Env = append(os.Environ(),
					"GODA_RTP="+godaRoot, "GODA_CMP_RTP="+repo, "GODA_CMP_BIN="+bin, "GODA_OUT="+out,
					"GODA_NO_PATCH_MAPS=1", "GODA_CMDLINE=1", "PUM_RTP="+pumDir)
				if real {
					cmd.Env = append(cmd.Env, "GODA_REAL_PUM=1")
				}
				if b, err := cmd.CombinedOutput(); err != nil {
					t.Fatalf("driver: %v\n%s", err, b)
				}
				raw, err := os.ReadFile(out)
				if err != nil {
					t.Fatalf("nothing recorded: %v", err)
				}
				var ds []dump
				for l := range strings.SplitSeq(strings.TrimSpace(string(raw)), "\n") {
					var d dump
					if err := json.Unmarshal([]byte(l), &d); err != nil {
						t.Fatalf("bad record %q: %v", l, err)
					}
					ds = append(ds, d)
				}
				t.Logf("records: %+v", ds)
				// (The driver's closing ":qa!" adds one more "left".)
				if len(ds) < 3 {
					t.Fatalf("got %d records, want 3", len(ds))
				}
				if !ds[0].Visible || ds[0].Cmdline != "set filety" || !slices.Contains(ds[0].Words, "filetype") {
					t.Errorf(`":set filety": %+v`, ds[0])
				}
				if !ds[1].Left {
					t.Errorf("leaving the command line was not reported: %+v", ds[1])
				}
				if ds[2].Visible || ds[2].Cmdline != "filety" {
					t.Errorf(`"/filety" (not enabled): %+v`, ds[2])
				}
				if real {
					// ds[3] left "/"? No: "/" was not enabled. ds[3]: after
					// Ctrl-N, ds[4]: after Ctrl-Y.
					if len(ds) < 5 {
						t.Fatalf("got %d records, want 5", len(ds))
					}
					if ds[3].Cmdline != "set filetype" || !ds[3].Visible {
						t.Errorf("after Ctrl-N: %+v", ds[3])
					}
					if ds[4].Cmdline != "set filetype" || ds[4].Visible {
						t.Errorf("after Ctrl-Y: %+v", ds[4])
					}
					// The cmdline source previews the help of "filetype"
					// (with a pum.vim that has the previewer option).
					if previewer(t, pumDir) && ds[3].Preview != "help" {
						t.Errorf("no help preview after Ctrl-N: %+v", ds[3])
					}
					// The command run before is offered by the history
					// source, from the word under the cursor on.
					i := slices.IndexFunc(ds, func(d dump) bool { return d.Cmdline == "let g:hist_m" })
					if i < 0 || !ds[i].Visible || !slices.Contains(ds[i].Words, "hist_marker = 1") {
						t.Errorf(`":let g:hist_m": %+v`, ds)
					}
				}
			})
		}
	})
}

// supportsPum reports whether the editor is new enough for pum.vim.
// previewer reports whether the pum.vim in dir has the previewer option.
func previewer(t *testing.T, dir string) bool {
	b, err := os.ReadFile(filepath.Join(dir, "autoload", "pum.vim"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), "previewer:") {
		t.Log("pum.vim has no previewer option: the preview is not checked")
		return false
	}
	return true
}

func supportsPum(ed e2e.Editor) bool {
	script := filepath.Join(os.TempDir(), "goda-cmp-pumcheck.vim")
	if err := os.WriteFile(script, []byte("if has('patch-9.1.0448') || has('nvim-0.10')\n  qa!\nendif\ncq!\n"), 0o644); err != nil {
		return false
	}
	defer os.Remove(script)
	return exec.Command(ed.Bin, ed.Headless(script)...).Run() == nil
}

// runDriver types steps into ed with the test vimrc and returns the records.
func runDriver(t *testing.T, ed e2e.Editor, bin, repo, godaRoot, file, steps string, env ...string) []dump {
	t.Helper()
	return runDriverIn(t, "", ed, bin, repo, godaRoot, file, steps, env...)
}

// runDriverIn is runDriver with the editor started in the directory dir.
func runDriverIn(t *testing.T, dir string, ed e2e.Editor, bin, repo, godaRoot, file, steps string, env ...string) []dump {
	t.Helper()
	out := filepath.Join(t.TempDir(), "out.jsonl")
	testdata, _ := filepath.Abs("testdata")
	cmd := exec.Command("python3", "-I", filepath.Join(testdata, "drive.py"), ed.Bin, filepath.Join(testdata, "vimrc.vim"), file, steps)
	cmd.Dir = dir
	cmd.Env = append(append(os.Environ(),
		"GODA_RTP="+godaRoot, "GODA_CMP_RTP="+repo, "GODA_CMP_BIN="+bin, "GODA_OUT="+out,
		"GODA_NO_PATCH_MAPS=1"), env...)
	if b, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("driver: %v\n%s", err, b)
	}
	raw, err := os.ReadFile(out)
	if err != nil {
		t.Fatalf("nothing recorded: %v", err)
	}
	var ds []dump
	for l := range strings.SplitSeq(strings.TrimSpace(string(raw)), "\n") {
		var d dump
		if err := json.Unmarshal([]byte(l), &d); err != nil {
			t.Fatalf("bad record %q: %v", l, err)
		}
		ds = append(ds, d)
	}
	for i, d := range ds {
		if d.Current != nil {
			t.Logf("record %d: %+v current %+v", i, d, *d.Current)
		} else {
			t.Logf("record %d: %+v", i, d)
		}
	}
	return ds
}

// TestRealFunctions: goda_cmp#map#insert_item() and
// goda_cmp#map#complete_common_string(), an alias with params of its own set
// from Vim script, goda_cmp#get_current() and goda_cmp#visible(), and
// contextFiletype "treesitter" (Neovim) in a real editor.
func TestRealFunctions(t *testing.T) {
	if testing.Short() {
		t.Skip("builds a binary and starts an editor")
	}
	if _, err := exec.LookPath("python3"); err != nil {
		t.Skip("python3 is needed to drive the pty")
	}
	bin, repo, godaRoot := buildConfig(t, "testdata/funcscfg")
	e2e.Run(t, func(t *testing.T, ed e2e.Editor) {
		file := filepath.Join(t.TempDir(), "t.md")
		text := "alphabet_long_word alpha alphabet\n\n```lua\nlocal x = 1\n```\n"
		if err := os.WriteFile(file, []byte(text), 0o644); err != nil {
			t.Fatal(err)
		}
		// 1. On the empty line 2, "alp": around and around2 (which takes
		//    words of 9 characters or more, and keeps duplicates).
		// 2. Ctrl-X 2: the common prefix, "alpha".
		// 3. Ctrl-X 1: the first item.
		// 4. Ctrl-X 3 on line 2 and in the lua block: the configuration.
		steps := `[
		  ["2GA", 0.3], ["alp", 1.5], ["\u001d", 0.3], ["\u00183", 0.3],
		  ["\u00182", 1.5], ["\u001d", 0.3],
		  ["\u00181", 1.0], ["\u001d", 0.3],
		  ["\u001b", 0.3],
		  ["2GA\u00183", 0.5], ["\u001b", 0.3],
		  ["4GA\u00183", 0.5], ["\u001b", 0.3]
		]`
		ds := runDriver(t, ed, bin, repo, godaRoot, file, steps, "PUM_RTP=", "GODA_FUNCS=1")
		if len(ds) < 6 {
			t.Fatalf("got %d records, want 6", len(ds))
		}
		n := 0
		for _, w := range ds[0].Words {
			if w == "alphabet_long_word" {
				n++
			}
		}
		if !ds[0].Visible || n != 2 || !slices.Contains(ds[0].Words, "alpha") {
			t.Errorf(`"alp": %+v`, ds[0])
		}
		if ds[1].Current == nil || !ds[1].Current.Visible {
			t.Errorf("goda_cmp#visible() with the popup open: %+v", ds[1])
		}
		if ds[2].Line != "alpha" {
			t.Errorf("complete_common_string: %+v", ds[2])
		}
		if ds[3].Line != "alphabet_long_word" {
			t.Errorf("insert_item(0): %+v", ds[3])
		}
		if ds[4].Current == nil || ds[4].Current.Mark != "" {
			t.Errorf("markdown: %+v", ds[4])
		}
		want := ""
		if ed.Name == "nvim" {
			want = "LUA" // the lua block, by tree-sitter
		}
		if ds[5].Current == nil || ds[5].Current.Mark != want {
			t.Errorf("in the lua block: %+v, want mark %q", ds[5], want)
		}
	})
}

// TestRealTerminal completes in a terminal buffer with pum.vim: words the
// shell printed, and accepting one sends it to the shell.
func TestRealTerminal(t *testing.T) {
	if testing.Short() {
		t.Skip("builds a binary and starts an editor")
	}
	if _, err := exec.LookPath("python3"); err != nil {
		t.Skip("python3 is needed to drive the pty")
	}
	pumDir := os.Getenv("GODA_CMP_PUM")
	if pumDir == "" {
		t.Skip("GODA_CMP_PUM is not set")
	}
	bin, repo, godaRoot := buildConfig(t, "testdata/termcfg")
	e2e.Run(t, func(t *testing.T, ed e2e.Editor) {
		if !supportsPum(ed) {
			t.Skip("this editor is too old for pum.vim")
		}
		script := filepath.Join(t.TempDir(), "check.vim")
		if err := os.WriteFile(script, []byte("if exists('##TextChangedT')\n  qa!\nendif\ncq!\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		if exec.Command(ed.Bin, ed.Headless(script)...).Run() != nil {
			t.Skip("this editor has no TextChangedT")
		}
		file := filepath.Join(t.TempDir(), "t.txt")
		if err := os.WriteFile(file, []byte("x\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		// Print a word, then type its start; Ctrl-N selects the item, Ctrl-Y
		// sends it to the shell, which pum.vim does with Ctrl-H for the
		// typed part (hence stty erase).
		steps := `[
		  [":GodaTerm\r", 2.0], ["stty erase '^H'\r", 0.5], ["echo zebra_unique_word\r", 1.0],
		  ["echo zeb", 1.5], ["\u001d", 0.3],
		  ["\u000e", 0.8], ["\u001d", 0.3], ["\u0019", 0.8], ["\u001d", 0.3],
		  ["\u001c\u000e", 0.3]
		]`
		ds := runDriver(t, ed, bin, repo, godaRoot, file, steps, "PUM_RTP="+pumDir, "GODA_TERMINAL=1", "PS1=$ ", "ENV=")
		if len(ds) < 3 {
			t.Fatalf("got %d records, want 3", len(ds))
		}
		if !ds[0].Visible || !slices.Contains(ds[0].Words, "zebra_unique_word") {
			t.Errorf(`"echo zeb": %+v`, ds[0])
		}
		if !strings.HasSuffix(strings.TrimRight(ds[2].Line, " "), "echo zebra_unique_word") || ds[2].Visible {
			t.Errorf("after accepting: %+v", ds[2])
		}
	})
}

// TestRealPorts uses the ported extensions in a real editor, configured as a
// ddc.vim user would: buffer, register (its words), file and input sources,
// the matchers, sorters and converters of "_", pum.vim, and the inline UI for
// a single candidate, inserted by goda_cmp#map#insert_item(0).
func TestRealPorts(t *testing.T) {
	if testing.Short() {
		t.Skip("builds a binary and starts an editor")
	}
	if _, err := exec.LookPath("python3"); err != nil {
		t.Skip("python3 is needed to drive the pty")
	}
	pumDir := os.Getenv("GODA_CMP_PUM")
	if pumDir == "" {
		t.Skip("GODA_CMP_PUM is not set")
	}
	bin, repo, godaRoot := buildConfig(t, "testdata/portcfg")
	e2e.Run(t, func(t *testing.T, ed e2e.Editor) {
		if !supportsPum(ed) {
			t.Skip("this editor is too old for pum.vim")
		}
		dir := t.TempDir()
		for name, text := range map[string]string{
			"t.txt":        "uniqueword_alpha\n\n",
			"other.txt":    "otherbufword_zz\n",
			"foo_file.txt": "",
		} {
			if err := os.WriteFile(filepath.Join(dir, name), []byte(text), 0o644); err != nil {
				t.Fatal(err)
			}
		}
		if err := os.Mkdir(filepath.Join(dir, "foo_dir"), 0o755); err != nil {
			t.Fatal(err)
		}
		// Each single candidate is shown inline and inserted with Ctrl-G.
		steps := `[
		  [":split other.txt\r", 0.5], [":wincmd p\r", 0.5],
		  ["2GA", 0.3], ["uniquew", 1.5], ["\u001d", 0.3], ["\u0007", 0.8], ["\u001d", 0.3],
		  ["\u001bo", 0.3], ["otherbufw", 1.5], ["\u001d", 0.3], ["\u0007", 0.8], ["\u001d", 0.3],
		  ["\u001bo", 0.3], ["registerw", 1.5], ["\u001d", 0.3], ["\u0007", 0.8], ["\u001d", 0.3],
		  ["\u001bo", 0.3], ["./foo_", 1.5], ["\u001d", 0.3],
		  ["\u001b", 0.5],
		  [":let g:r = input('> ', '', 'file')\r", 0.8],
		  ["foo_f", 1.5], ["\u001d", 0.3], ["\u0007", 0.8], ["\u001d", 0.3],
		  ["\u001b", 0.3],
		  [":hide edit x.vim\r", 0.5], ["iset filety", 1.5], ["\u001d", 0.3], ["\u0007", 0.8], ["\u001d", 0.3],
		  ["\u001bo", 0.3], ["\u0012=", 0.5], ["strle", 1.5], ["\u001d", 0.3],
		  ["\u001b", 0.3], ["\u001b", 0.3]
		]`
		ds := runDriverIn(t, dir, ed, bin, repo, godaRoot, "t.txt", steps, "PUM_RTP="+pumDir, "GODA_PORTS=1")
		if len(ds) < 9 {
			t.Fatalf("got %d records, want 9", len(ds))
		}
		for i, word := range []string{"uniqueword_alpha", "otherbufword_zz", "registerword_qq"} {
			shownInline, inserted := ds[2*i], ds[2*i+1]
			if !shownInline.Inline {
				t.Errorf("%s: not shown inline: %+v", word, shownInline)
			}
			if inserted.Line != word {
				t.Errorf("%s: after Ctrl-G: %+v", word, inserted)
			}
		}
		if d := ds[6]; !d.Visible || !slices.Contains(d.Words, "foo_file.txt") || !slices.Contains(d.Words, "foo_dir") {
			t.Errorf(`"./foo_": %+v`, d)
		}
		// input(), without goda_cmp#enable_cmdline().
		if d := ds[7]; !d.Inline || d.Cmdline != "foo_f" {
			t.Errorf(`input() "foo_f": %+v`, d)
		}
		if d := ds[8]; d.Cmdline != "foo_file.txt" {
			t.Errorf("input() after Ctrl-G: %+v", d)
		}
		// Vim script: the option.
		if len(ds) < 11 {
			t.Fatalf("got %d records, want 11", len(ds))
		}
		if d := ds[9]; !d.Inline && !slices.Contains(d.Words, "filetype") {
			t.Errorf(`"set filety": %+v`, d)
		}
		if d := ds[10]; d.Line != "set filetype" {
			t.Errorf(`"set filety" after Ctrl-G: %+v`, d)
		}
		// The expression register (Ctrl-R =), without
		// goda_cmp#enable_cmdline().
		if len(ds) < 12 {
			t.Fatalf("got %d records, want 12", len(ds))
		}
		if d := ds[11]; d.Cmdline != "strle" || (!d.Inline && !slices.Contains(d.Words, "strlen(")) {
			t.Errorf(`"=strle": %+v`, d)
		}
	})
}

// TestRealShellNative completes in a terminal with zsh's own completion
// (ext/source/shellnative).
func TestRealShellNative(t *testing.T) {
	if testing.Short() {
		t.Skip("builds a binary and starts an editor")
	}
	if _, err := exec.LookPath("python3"); err != nil {
		t.Skip("python3 is needed to drive the pty")
	}
	if _, err := exec.LookPath("zsh"); err != nil {
		t.Skip("zsh is not installed")
	}
	pumDir := os.Getenv("GODA_CMP_PUM")
	if pumDir == "" {
		t.Skip("GODA_CMP_PUM is not set")
	}
	bin, repo, godaRoot := buildConfig(t, "testdata/portcfg")
	e2e.Run(t, func(t *testing.T, ed e2e.Editor) {
		if !supportsPum(ed) {
			t.Skip("this editor is too old for pum.vim")
		}
		dir := t.TempDir()
		if err := os.WriteFile(filepath.Join(dir, "t.txt"), []byte("x\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		// The first answer of zsh may be slower than hideTimeout: a second
		// keystroke asks again.
		steps := `[
		  [":GodaTerm\r", 2.0], ["printf ok; ec", 2.5], ["h", 1.5], ["\u001d", 0.3],
		  ["\u001c\u000e", 0.3]
		]`
		ds := runDriverIn(t, dir, ed, bin, repo, godaRoot, "t.txt", steps,
			"PUM_RTP="+pumDir, "GODA_PORTS=1", "GODA_TERMINAL=1", "PS1=$ ", "ENV=")
		if len(ds) < 1 {
			t.Fatal("nothing recorded")
		}
		if !ds[0].Visible || !slices.Contains(ds[0].Words, "echo") {
			t.Errorf(`"ech": %+v`, ds[0])
		}
	})
}

// TestExampleDDC builds examples/ddc (a translation of a ddc.vim user's
// configuration) and asks the binary whether its configuration is valid.
func TestExampleDDC(t *testing.T) {
	if testing.Short() {
		t.Skip("builds a binary")
	}
	bin, _, _ := buildConfig(t, "../examples/ddc")
	cmd := exec.Command(bin, "--transport=jsonl")
	cmd.Stdin = strings.NewReader(`{"id":1,"method":"cmp.status","params":[]}` + "\n")
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("%v: %s", err, out)
	}
	var res struct {
		Result string `json:"result"`
	}
	if err := json.Unmarshal(out, &res); err != nil || res.Result != "" {
		t.Errorf("status: %s (%v)", out, err)
	}
}

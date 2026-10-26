package cmp_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/Shougo/goda-cmp.vim/cmp/cmptest/e2e"
)

// The end-to-end tests of the core run with the test UI
// (cmptest.NewUI("test")), whose state the editor records; the extensions
// have their own (ext/e2e).

// TestRealPatch types into a real editor and checks what the UI is asked to
// show, with the buffer patched at run time: switched off, put back, and an
// invalid patch refused.
func TestRealPatch(t *testing.T) {
	if testing.Short() {
		t.Skip("builds a binary and starts an editor")
	}
	bin := e2e.BuildConfig(t, "testdata/corecfg")
	e2e.Run(t, func(t *testing.T, ed e2e.Editor) {
		text := filepath.Join(t.TempDir(), "text.txt")
		if err := os.WriteFile(text, []byte("foobar\nfoo_baz\nfoobarbaz\nzzz\n\n"), 0o644); err != nil {
			t.Fatal(err)
		}
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
		ds := e2e.Drive(t, ed, e2e.Options{Bin: bin, File: text, Steps: steps,
			Env: []string{"GODA_DUMP_UI=test", "GODA_PATCH_MAPS=1"}})
		want := []e2e.Dump{
			{Visible: true, Words: []string{"foobar", "foo_baz", "foobarbaz"}, Line: "fo"},
			{Visible: true, Words: []string{"foobar", "foo_baz", "foobarbaz"}, Line: "foo"},
			{Visible: true, Words: []string{"foobar", "foobarbaz"}, Line: "foob"},
			{Visible: false, Words: nil, Line: "qq"}, // nothing in the buffer starts with qq
			// Sources switched off for the buffer by a run-time patch.
			{Visible: false, Words: nil, Line: "fo"},
			// Put back: completion works again.
			{Visible: true, Words: []string{"foobar", "foo_baz", "foobarbaz", "foob"}, Line: "foo"},
		}
		if len(ds) != len(want)+1 {
			t.Fatalf("got %d records, want %d", len(ds), len(want)+1)
		}
		for i, w := range want {
			g := ds[i]
			if g.Visible != w.Visible || (len(g.Words)+len(w.Words) > 0 && !slices.Equal(g.Words, w.Words)) || g.Line != w.Line {
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
	})
}

// TestRealCmdline: command-line completion in a real editor. ":" enables it
// for that command line (as in ddc.vim's documentation), and leaving the
// command line ends it: the next one ("/") is not completed.
func TestRealCmdline(t *testing.T) {
	if testing.Short() {
		t.Skip("builds a binary and starts an editor")
	}
	bin := e2e.BuildConfig(t, "testdata/corecfg")
	vimrc, _ := filepath.Abs("testdata/cmdline.vim")
	e2e.Run(t, func(t *testing.T, ed e2e.Editor) {
		text := filepath.Join(t.TempDir(), "text.txt")
		if err := os.WriteFile(text, []byte("x\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		steps := `[
		  [":", 0.5], ["set filety", 1.5], ["\u001d", 0.3],
		  ["\u001b", 0.5],
		  ["/filety", 1.0], ["\u001d", 0.3],
		  ["\u001b", 0.3]
		]`
		ds := e2e.Drive(t, ed, e2e.Options{Bin: bin, File: text, Steps: steps, Vimrc: vimrc,
			Env: []string{"GODA_DUMP_UI=test", "GODA_WORDS=filetype fileformat"}})
		// (The driver's closing ":qa!" adds one more "left".)
		if len(ds) < 3 {
			t.Fatalf("got %d records, want 3", len(ds))
		}
		if !ds[0].Visible || ds[0].Cmdline != "set filety" || !slices.Equal(ds[0].Words, []string{"filetype"}) {
			t.Errorf(`":set filety": %+v`, ds[0])
		}
		if !ds[1].Left {
			t.Errorf("leaving the command line was not reported: %+v", ds[1])
		}
		if ds[2].Visible || ds[2].Cmdline != "filety" {
			t.Errorf(`"/filety" (not enabled): %+v`, ds[2])
		}
	})
}

// TestRealTerminal completes in a terminal buffer.
func TestRealTerminal(t *testing.T) {
	if testing.Short() {
		t.Skip("builds a binary and starts an editor")
	}
	bin := e2e.BuildConfig(t, "testdata/corecfg")
	vimrc, _ := filepath.Abs("testdata/terminal.vim")
	e2e.Run(t, func(t *testing.T, ed e2e.Editor) {
		if !ed.Has("exists('##TextChangedT')") {
			t.Skip("this editor has no TextChangedT")
		}
		file := filepath.Join(t.TempDir(), "t.txt")
		if err := os.WriteFile(file, []byte("x\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		steps := `[
		  [":GodaTerm\r", 2.0], ["echo zeb", 1.5], ["\u001d", 0.3],
		  ["\u001c\u000e", 0.3]
		]`
		ds := e2e.Drive(t, ed, e2e.Options{Bin: bin, File: file, Steps: steps, Vimrc: vimrc,
			Env: []string{"GODA_DUMP_UI=test", "GODA_WORDS=zebra_unique_word", "PS1=$ ", "ENV="}})
		if len(ds) < 1 {
			t.Fatal("nothing recorded")
		}
		if !ds[0].Visible || !slices.Equal(ds[0].Words, []string{"zebra_unique_word"}) {
			t.Errorf(`"echo zeb": %+v`, ds[0])
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
	bin := e2e.BuildConfig(t, "testdata/eventcfg")
	e2e.Run(t, func(t *testing.T, ed e2e.Editor) {
		dir := t.TempDir()
		a := filepath.Join(dir, "a.txt")
		if err := os.WriteFile(a, []byte("a\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		events := filepath.Join(dir, "events.txt")
		// a.txt (current at start), c.md (not text), b.txt, then :bd of b.txt.
		steps := `[
		  [":e ` + filepath.Join(dir, "c.md") + `\r", 0.5],
		  [":e ` + filepath.Join(dir, "b.txt") + `\r", 0.5],
		  [":bd\r", 1.0],
		  ["i\u001d\u001b", 0.3]
		]`
		e2e.Drive(t, ed, e2e.Options{Bin: bin, File: a, Steps: steps,
			Env: []string{"GODA_DUMP_UI=test", "GODA_EVENT_FILE=" + events}})
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
	bin := e2e.BuildConfig(t, "testdata/badcfg")
	repo, godaRoot := e2e.Roots(t)
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
	built := e2e.BuildConfig(t, "testdata/nocmp")
	repo, godaRoot := e2e.Roots(t)
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

// TestRealFunctions: goda_cmp#map#insert_item() and
// goda_cmp#map#complete_common_string(), an alias with params of its own set
// from Vim script, goda_cmp#get_current() and goda_cmp#visible(), and
// contextFiletype "treesitter" (Neovim) in a real editor.
func TestRealFunctions(t *testing.T) {
	if testing.Short() {
		t.Skip("builds a binary and starts an editor")
	}
	bin := e2e.BuildConfig(t, "testdata/funcscfg")
	vimrc, _ := filepath.Abs("testdata/funcs.vim")
	e2e.Run(t, func(t *testing.T, ed e2e.Editor) {
		file := filepath.Join(t.TempDir(), "t.md")
		text := "alphabet_long_word alpha alphabet\n\n```lua\nlocal x = 1\n```\n"
		if err := os.WriteFile(file, []byte(text), 0o644); err != nil {
			t.Fatal(err)
		}
		// 1. On the empty line 2, "alp": words and words2 (which takes
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
		ds := e2e.Drive(t, ed, e2e.Options{Bin: bin, File: file, Steps: steps, Vimrc: vimrc,
			Env: []string{"GODA_DUMP_UI=test"}})
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

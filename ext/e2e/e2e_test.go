// Package e2e_test holds the end-to-end tests of the extensions together: the
// UIs in a real editor (Vim's popup, pum.vim, inline), the ported sources and
// filters, and the example configurations. Each runs in Vim and Neovim,
// whichever are installed; GODA_CMP_PUM names a checkout of pum.vim for the
// scenarios that need it.
package e2e_test

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/Shougo/goda-cmp.vim/cmp/cmptest/e2e"
)

// scenario is the Vim script of these tests (sourced by e2e's vimrc).
func scenario(t *testing.T) string {
	p, err := filepath.Abs("testdata/scenario.vim")
	if err != nil {
		t.Fatal(err)
	}
	return p
}

// pumOptions adds pum.vim in dir ("": none) to o.
func pumOptions(o e2e.Options, dir string) e2e.Options {
	if dir != "" {
		o.RTP = append(o.RTP, dir)
	}
	o.Env = append(o.Env, "PUM_RTP="+dir)
	return o
}

// supportsPum reports whether the editor is new enough for pum.vim.
func supportsPum(ed e2e.Editor) bool {
	return ed.Has("has('patch-9.1.0448') || has('nvim-0.10')")
}

// realPum is pum.vim of GODA_CMP_PUM, for an editor new enough for it;
// otherwise the test is skipped.
func realPum(t *testing.T, ed e2e.Editor) string {
	t.Helper()
	dir := os.Getenv("GODA_CMP_PUM")
	if dir == "" {
		t.Skip("GODA_CMP_PUM is not set")
	}
	if !supportsPum(ed) {
		t.Skip("this editor is too old for pum.vim")
	}
	return dir
}

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

// TestExampleConfig types into a real editor with a binary built from
// examples/config and checks what the popup shows: Vim's own popup (the
// native UI), and the pum UI with a stand-in for pum.vim and with pum.vim.
func TestExampleConfig(t *testing.T) {
	if testing.Short() {
		t.Skip("builds a binary and starts an editor")
	}
	bin := e2e.BuildConfig(t, "../../examples/config")
	text := filepath.Join(t.TempDir(), "text.txt")
	if err := os.WriteFile(text, []byte("foobar\nfoo_baz\nfoobarbaz\nzzz\n\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	e2e.Run(t, func(t *testing.T, ed e2e.Editor) {
		run := func(t *testing.T, pumDir, completeopt string) []e2e.Dump {
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
			return e2e.Drive(t, ed, pumOptions(e2e.Options{Bin: bin, File: text, Steps: steps,
				Vimrc: scenario(t), Env: []string{"GODA_PATCH_MAPS=1", "GODA_COMPLETEOPT=" + completeopt}}, pumDir))
		}
		check := func(t *testing.T, ds []e2e.Dump) {
			want := []e2e.Dump{
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
				if g.Visible != w.Visible || (len(g.Words)+len(w.Words) > 0 && !slices.Equal(g.Words, w.Words)) || g.Line != w.Line {
					t.Errorf("record %d:\n got %+v\nwant %+v", i, g, w)
				}
			}
			if !strings.Contains(ds[len(want)].Error, "unknown source") {
				t.Errorf("invalid patch: %+v", ds[len(want)])
			}
		}

		t.Run("native", func(t *testing.T) { check(t, run(t, "", "")) })
		// The popup must never change the text, whatever the user's
		// 'completeopt' is: without noinsert the first candidate would be
		// inserted, and with longest the common prefix.
		t.Run("native-with-default-completeopt", func(t *testing.T) { check(t, run(t, "", "menu,preview,longest")) })
		// The stand-in checks that the pum UI calls pum#open(startcol,
		// items) with the right items; it cannot check how pum.vim draws
		// them.
		fake, _ := filepath.Abs("testdata/fakepum")
		t.Run("pum-with-stand-in", func(t *testing.T) { check(t, run(t, fake, "")) })
		if pum := os.Getenv("GODA_CMP_PUM"); pum != "" && supportsPum(ed) {
			t.Run("pum", func(t *testing.T) { check(t, run(t, pum, "")) })
		}
	})
}

// TestRealStream: partial results arrive in a real Vim, a popup the user is
// choosing from is left alone by later updates (the native UI), and
// accepting an item reaches the source's OnCompleteDone with the item's
// UserData.
func TestRealStream(t *testing.T) {
	if testing.Short() {
		t.Skip("builds a binary and starts an editor")
	}
	bin := e2e.BuildConfig(t, "testdata/streamcfg")
	e2e.Run(t, func(t *testing.T, ed e2e.Editor) {
		dir := t.TempDir()
		text := filepath.Join(dir, "text.txt")
		if err := os.WriteFile(text, []byte("\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		done := filepath.Join(dir, "done.txt")
		// 1. type "al": the fast answer is shown, then the complete one.
		// 2. new line, type "al", select the first item while only the fast
		//    answer is there, wait for the late answer (must not drop the
		//    selection), accept with Ctrl-Y.
		steps := `[
		  ["Oal", 0.5], ["\u001d", 0.2],
		  ["", 1.2], ["\u001d", 0.2],
		  ["\u001b", 0.3],
		  ["o", 0.2], ["al", 0.5], ["\u000e", 0.3],
		  ["", 1.2], ["\u001d", 0.2],
		  ["\u0019", 0.5], ["\u001d", 0.2],
		  ["\u001b", 0.3]
		]`
		ds := e2e.Drive(t, ed, e2e.Options{Bin: bin, File: text, Steps: steps,
			Env: []string{"GODA_DONE_FILE=" + done}})
		if len(ds) != 4 {
			t.Fatalf("got %d records, want 4", len(ds))
		}
		// Fast answer first (items are listed in popup order), complete one
		// later.
		if !ds[0].Visible || !slices.Equal(ds[0].Words, []string{"alpha_fast"}) {
			t.Errorf("partial update: %+v", ds[0])
		}
		if !ds[1].Visible || !slices.Equal(ds[1].Words, []string{"alpha_fast", "alpha_late"}) {
			t.Errorf("final update: %+v", ds[1])
		}
		// Selecting inserted alpha_fast; the late answer did not take the
		// popup away from under the user (the native popup cannot be
		// updated while an item is selected).
		if ds[2].Line != "alpha_fast" || !ds[2].Visible || !slices.Equal(ds[2].Words, []string{"alpha_fast"}) {
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

// TestRealCmdline: command-line completion with the pum UI. ":" enables it
// for that command line, the cmdline source offers what getcompletion()
// does, and leaving the command line ends it. It runs with the pum.vim
// stand-in, and with pum.vim itself (selecting, accepting, the preview and
// the history source) when GODA_CMP_PUM names a checkout.
func TestRealCmdline(t *testing.T) {
	if testing.Short() {
		t.Skip("builds a binary and starts an editor")
	}
	bin := e2e.BuildConfig(t, "testdata/cmdlinecfg")
	fake, _ := filepath.Abs("testdata/fakepum")
	e2e.Run(t, func(t *testing.T, ed e2e.Editor) {
		pums := map[string]string{"pum-stand-in": fake}
		if real := os.Getenv("GODA_CMP_PUM"); real != "" && supportsPum(ed) {
			pums["pum"] = real
		}
		for name, pumDir := range pums {
			t.Run(name, func(t *testing.T) {
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
				real := name == "pum"
				env := []string{"GODA_CMDLINE=1"}
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
					env = append(env, "GODA_REAL_PUM=1")
				}
				ds := e2e.Drive(t, ed, pumOptions(e2e.Options{Bin: bin, File: text, Steps: steps,
					Vimrc: scenario(t), Env: env}, pumDir))
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
				if !real {
					return
				}
				// ds[3]: after Ctrl-N, ds[4]: after Ctrl-Y.
				if len(ds) < 5 {
					t.Fatalf("got %d records, want 5", len(ds))
				}
				if ds[3].Cmdline != "set filetype" || !ds[3].Visible {
					t.Errorf("after Ctrl-N: %+v", ds[3])
				}
				if ds[4].Cmdline != "set filetype" || ds[4].Visible {
					t.Errorf("after Ctrl-Y: %+v", ds[4])
				}
				// The cmdline source previews the help of "filetype" (with a
				// pum.vim that has the previewer option).
				if previewer(t, pumDir) && ds[3].Preview != "help" {
					t.Errorf("no help preview after Ctrl-N: %+v", ds[3])
				}
				// The command run before is offered by the history source,
				// from the word under the cursor on.
				i := slices.IndexFunc(ds, func(d e2e.Dump) bool { return d.Cmdline == "let g:hist_m" })
				if i < 0 || !ds[i].Visible || !slices.Contains(ds[i].Words, "hist_marker = 1") {
					t.Errorf(`":let g:hist_m": %+v`, ds)
				}
			})
		}
	})
}

// TestRealTerminal completes in a terminal buffer with pum.vim: words the
// shell printed, and accepting one sends it to the shell.
func TestRealTerminal(t *testing.T) {
	if testing.Short() {
		t.Skip("builds a binary and starts an editor")
	}
	bin := e2e.BuildConfig(t, "testdata/termcfg")
	e2e.Run(t, func(t *testing.T, ed e2e.Editor) {
		pumDir := realPum(t, ed)
		if !ed.Has("exists('##TextChangedT')") {
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
		ds := e2e.Drive(t, ed, pumOptions(e2e.Options{Bin: bin, File: file, Steps: steps,
			Vimrc: scenario(t), Env: []string{"GODA_TERMINAL=1", "PS1=$ ", "ENV="}}, pumDir))
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
	bin := e2e.BuildConfig(t, "testdata/portcfg")
	e2e.Run(t, func(t *testing.T, ed e2e.Editor) {
		pumDir := realPum(t, ed)
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
		ds := e2e.Drive(t, ed, pumOptions(e2e.Options{Bin: bin, File: "t.txt", Dir: dir, Steps: steps,
			Vimrc: scenario(t), Env: []string{"GODA_PORTS=1"}}, pumDir))
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
	if _, err := exec.LookPath("zsh"); err != nil {
		t.Skip("zsh is not installed")
	}
	bin := e2e.BuildConfig(t, "testdata/portcfg")
	e2e.Run(t, func(t *testing.T, ed e2e.Editor) {
		pumDir := realPum(t, ed)
		dir := t.TempDir()
		if err := os.WriteFile(filepath.Join(dir, "t.txt"), []byte("x\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		// The first request starts zsh, which may take a while on a loaded
		// machine; the next keystrokes ask again.
		steps := `[
		  [":GodaTerm\r", 2.0], ["printf ok; e", 5.0], ["c", 2.0], ["h", 2.5], ["\u001d", 0.3],
		  ["\u001c\u000e", 0.3]
		]`
		ds := e2e.Drive(t, ed, pumOptions(e2e.Options{Bin: bin, File: "t.txt", Dir: dir, Steps: steps,
			Vimrc: scenario(t), Env: []string{"GODA_PORTS=1", "GODA_TERMINAL=1", "PS1=$ ", "ENV="}}, pumDir))
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
	bin := e2e.BuildConfig(t, "../../examples/ddc")
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

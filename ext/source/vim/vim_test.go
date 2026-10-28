package vim

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/Shougo/goda-cmp.vim/cmp"
	"github.com/Shougo/goda.vim/sdk"
)

// vimHost answers each request with a Vim of its own, started on file with
// the cursor at line lnum.
type vimHost struct {
	t    *testing.T
	file string
	lnum int
	pre  []string // commands run first
}

func (vimHost) Meta() sdk.Meta { return sdk.Meta{} }

func (h vimHost) Eval(ctx context.Context, expr string) (any, error) {
	h.t.Helper()
	dir := h.t.TempDir()
	out := filepath.Join(dir, "out")
	script := filepath.Join(dir, "x.vim")
	lines := append(slices.Clone(h.pre),
		"filetype plugin on",
		"edit "+h.file,
		"call cursor("+itoa(h.lnum)+", 1)",
		"try",
		"call writefile([json_encode("+expr+")], '"+out+"')",
		"catch",
		"call writefile(['error', v:exception], '"+out+"')",
		"endtry",
		"qa!",
	)
	if err := os.WriteFile(script, []byte(strings.Join(lines, "\n")), 0o644); err != nil {
		return nil, err
	}
	cmd := exec.CommandContext(ctx, "vim", "-Nu", "NONE", "-i", "NONE", "-es", "-S", script)
	if b, err := cmd.CombinedOutput(); err != nil {
		h.t.Fatalf("vim: %v: %s", err, b)
	}
	b, err := os.ReadFile(out)
	if err != nil {
		h.t.Fatalf("%s: %v", expr, err)
	}
	if e, ok := strings.CutPrefix(string(b), "error\n"); ok {
		return nil, errors.New(e)
	}
	var v any
	return v, json.Unmarshal(b, &v)
}

func (h vimHost) Call(ctx context.Context, fn string, args ...any) (any, error) {
	if args == nil {
		args = []any{}
	}
	b, err := json.Marshal(args)
	if err != nil {
		return nil, err
	}
	return h.Eval(ctx, "call('"+fn+"', json_decode('"+strings.ReplaceAll(string(b), "'", "''")+"'))")
}

func (vimHost) Notify(context.Context, string, ...any) error { return nil }
func (vimHost) Cmd(context.Context, string) error            { return nil }

const script = `function s:outer(name, ...) abort
  let count = 10
  let [first, second] = s:pair()
  let text = 'abc'
  function s:nested() abort
    let hidden = 1
  endfunction
  for item in [1, 2]
    let total = 1.5
  endfor
  call s:x(
        \
endfunction

def Typed(width: number, height: number)
  var area = width * height

enddef
`

func TestGather(t *testing.T) {
	if testing.Short() {
		t.Skip("starts Vim")
	}
	if _, err := exec.LookPath("vim"); err != nil {
		t.Skip("no vim")
	}
	file := filepath.Join(t.TempDir(), "x.vim")
	if err := os.WriteFile(file, []byte(script), 0o644); err != nil {
		t.Fatal(err)
	}
	words := func(items []cmp.Item) []string {
		var out []string
		for _, it := range items {
			out = append(out, it.Word+"/"+it.Kind)
		}
		slices.Sort(out)
		return out
	}
	locals := []string{
		"a:0/0", "a:000/[]", "a:1/", "a:2/", "a:3/", "a:4/", "a:name/",
		"count/0", "first/", "second/",
	}
	for _, tc := range []struct {
		name  string
		lnum  int
		input string
		start int
		want  []string // Word/Kind (in any order); nil: only check has
		has   []string
	}{
		{name: "locals", lnum: 4, input: "  let z = co", start: 10, want: locals},
		// The function defined inside is skipped.
		{name: "after nested", lnum: 11, input: "  call s:x(", start: 7,
			want: append(slices.Clone(locals), `text/""`, "item/", "total/.")},
		// In the function defined inside, only its own.
		{name: "in nested", lnum: 7, input: "    echo x", start: 9, want: []string{"hidden/0"}},
		// After a function: none.
		{name: "after the function", lnum: 14, input: "echo x", start: 5},
		{name: "def", lnum: 18, input: "echo x", start: 5,
			want: []string{"area/", "height/", "width/"}},
		{name: "has", lnum: 1, input: "if has('pa", start: 8,
			want: []string{"patch-/", "patch/"}},
		{name: "expand", lnum: 1, input: "echo expand('<cf", start: 13,
			has: []string{"<cfile>/", ":p/"}},
		{name: "option", lnum: 1, input: "let x = &sw", start: 8,
			has: []string{"&shiftwidth/o", "&nowrap/o"}},
		{name: "option local", lnum: 1, input: "echo &l:sw", start: 5,
			has: []string{"&l:shiftwidth/o"}},
		{name: "mapping", lnum: 1, input: "nnoremap <sil", start: 9,
			has: []string{"<LeftMouse>/", "<Esc>/", "<buffer>/", "<silent>/", "<expr>/"}},
		{name: "environment", lnum: 1, input: "echo $HO", start: 5,
			has: []string{"$HOME/e"}},
		{name: "comment", lnum: 1, input: `" call s:x`, start: 10},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := vimHost{t: t, file: file, lnum: tc.lnum}
			g, err := New().(source).Gather(t.Context(), h, &cmp.Context{Input: tc.input, Mode: "i"})
			if err != nil {
				t.Fatal(err)
			}
			if g.Start != tc.start {
				t.Errorf("start = %d, want %d", g.Start, tc.start)
			}
			got := words(g.Items)
			if tc.has == nil {
				if want := slices.Sorted(slices.Values(tc.want)); !slices.Equal(got, want) {
					t.Errorf("got %q\nwant %q", got, tc.want)
				}
				return
			}
			for _, w := range tc.has {
				if !slices.Contains(got, w) {
					t.Errorf("%q is missing (%d items)", w, len(got))
				}
			}
		})
	}
}

func TestCurText(t *testing.T) {
	for _, tc := range []struct {
		input string
		prev  []string
		want  string
	}{
		{"echo a | call b", nil, "call b"},
		{"echo x<bar>call", nil, "call"},
		{`      \ = 1`, []string{"let x"}, "let x = 1"},
		{`\`, []string{"", "call f("}, "call f("},
		{"", []string{"call f(", ""}, ""},
		{"plain", []string{"let x = 1"}, "plain"},
		// Only spaces: the line above goes on.
		{"  ", []string{"call f("}, "call f(  "},
		// (Not "s": the original took "[\s\\]" for "s" or a backslash.)
		{"s", []string{"call f("}, "s"},
	} {
		if got := curText(tc.input, tc.prev); got != tc.want {
			t.Errorf("curText(%q, %q) = %q, want %q", tc.input, tc.prev, got, tc.want)
		}
	}
}

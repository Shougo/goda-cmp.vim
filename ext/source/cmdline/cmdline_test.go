package cmdline

import (
	"context"
	"reflect"
	"testing"

	"github.com/Shougo/goda-cmp.vim/cmp"
	"github.com/Shougo/goda-cmp.vim/cmp/cmptest"
)

// host answers getcompletion(input, 'cmdline') from a table, and getline()
// from lines.
func host(t *testing.T, completions map[string][]string, lines ...string) *cmptest.Host {
	t.Helper()
	h := &cmptest.Host{Funcs: map[string]func(...any) (any, error){
		"getcompletion": func(a ...any) (any, error) {
			if a[1] != "cmdline" {
				t.Errorf("getcompletion type = %v", a[1])
			}
			out := []any{}
			for _, w := range completions[a[0].(string)] {
				out = append(out, w)
			}
			return out, nil
		},
		"getline": func(a ...any) (any, error) {
			return lines[a[0].(int)-1], nil
		},
		"getcmdtype": func(...any) (any, error) { return "", nil },
	}}
	return h
}

func gather(t *testing.T, h *cmptest.Host, c *cmp.Context) (int, []string, []string) {
	t.Helper()
	g, err := New().Gather(context.Background(), h, c)
	if err != nil {
		t.Fatal(err)
	}
	var words, abbrs []string
	for _, it := range g.Items {
		words = append(words, it.Word)
		abbrs = append(abbrs, it.Abbr)
	}
	return g.Start, words, abbrs
}

func cmdline(input, typ string) *cmp.Context {
	return &cmp.Context{Mode: "c", CmdType: typ, Input: input, Col: len(input) + 1, Lnum: 1}
}

func TestGather(t *testing.T) {
	h := host(t, map[string][]string{
		"set filety":  {"filetype"},
		"e ~/foo/ba":  {"~/foo/bar/", "~/foo/baz.txt"},
		"set nonu":    {"number", "numberwidth"},
		"help buf":    {"buffers", "bufexists()", "nobuf", "Buffers"},
		"e **/foo":    {"a/foo.txt", "b/foobar/", "c/other.txt"},
		"call goda_c": {"goda_cmp#enable(", "goda_cmp#disable("},
	})
	for _, c := range []struct {
		input       string
		start       int
		words, abbr []string
	}{
		// The common prefix ends with what is typed: nothing to trim.
		{"set filety", 4, []string{"filetype"}, []string{""}},
		// Paths: the directory part the input ends with is trimmed, and a
		// directory is completed without its slash (shown with it).
		{"e ~/foo/ba", 8, []string{"bar", "baz.txt"}, []string{"bar/", ""}},
		// "no" options are not returned by getcompletion().
		{"set nonu", 4, []string{"nonumber", "nonumberwidth"}, []string{"", ""}},
		// ":help" results that do not start with the input are dropped.
		{"help buf", 5, []string{"buffers", "bufexists()", "Buffers"}, []string{"", "", ""}},
		// "**/foo": results are matched by their last path component (a
		// directory by its whole path without the slash, as the original).
		{"e **/foo", 5, []string{"a/foo.txt"}, []string{""}},
		{"call goda_c", 5, []string{"goda_cmp#enable(", "goda_cmp#disable("}, []string{"", ""}},
	} {
		start, words, abbr := gather(t, h, cmdline(c.input, ":"))
		if start != c.start || !reflect.DeepEqual(words, c.words) || !reflect.DeepEqual(abbr, c.abbr) {
			t.Errorf("%q: start %d words %q abbr %q, want %d %q %q", c.input, start, words, abbr, c.start, c.words, c.abbr)
		}
	}
}

func TestNoCompletionForSearchAndInput(t *testing.T) {
	h := host(t, map[string][]string{"foo": {"foobar"}})
	for _, typ := range []string{"/", "?", ">", "=", "@"} {
		if _, words, _ := gather(t, h, cmdline("foo", typ)); len(words) != 0 {
			t.Errorf("cmdtype %q: %q", typ, words)
		}
	}
	if _, words, _ := gather(t, h, cmdline("foo", ":")); !reflect.DeepEqual(words, []string{"foobar"}) {
		t.Errorf(": got %q", words)
	}
}

// In a Vim script buffer, continuation lines above join the command.
func TestInsertModeContinuation(t *testing.T) {
	h := host(t, map[string][]string{"call foo(1, 2, bar": {"bar_one", "bar_two"}},
		"call foo(1,", `  \ 2,`)
	c := &cmp.Context{Mode: "i", Input: `  \ bar`, Lnum: 3}
	if _, words, _ := gather(t, h, c); !reflect.DeepEqual(words, []string{"bar_one", "bar_two"}) {
		t.Errorf("words = %q", words)
	}
}

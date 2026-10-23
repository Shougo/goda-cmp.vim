package cmp_test

import (
	"context"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Shougo/goda-cmp.vim/cmp"
	"github.com/Shougo/goda-cmp.vim/cmp/cmptest"
	"github.com/Shougo/goda-cmp.vim/ext/filter/matcherhead"
	"github.com/Shougo/goda.vim/sdk"
)

// countingSource counts its Gathers and remembers the inputs it saw.
type countingSource struct {
	name       string
	items      []cmp.Item
	incomplete bool
	delay      time.Duration

	n      atomic.Int32
	mu     sync.Mutex
	inputs []string
}

func (s *countingSource) Name() string { return s.name }
func (s *countingSource) Gather(ctx context.Context, _ sdk.Host, c *cmp.Context) (cmp.Gathered, error) {
	s.n.Add(1)
	s.mu.Lock()
	s.inputs = append(s.inputs, c.Input)
	s.mu.Unlock()
	if s.delay > 0 {
		select {
		case <-time.After(s.delay):
		case <-ctx.Done():
			return cmp.Gathered{}, ctx.Err()
		}
	}
	return cmp.Gathered{Start: c.KeywordStart(), Items: s.items, Incomplete: s.incomplete}, nil
}

func words(ws ...string) []cmp.Item {
	var out []cmp.Item
	for _, w := range ws {
		out = append(out, cmp.Item{Word: w})
	}
	return out
}

// plugin builds the plugin with matcherhead and the given configuration.
func plugin(t *testing.T, cfg cmp.Config, srcs ...cmp.Source) sdk.Plugin {
	t.Helper()
	b := newBuilder()
	for _, s := range srcs {
		b.AddSource(s)
	}
	b.AddFilter(matcherhead.New(matcherhead.Params{}))
	b.PatchGlobal(cfg)
	p, err := b.Plugin()
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func call(t *testing.T, p sdk.Plugin, h sdk.Host, c map[string]any) {
	t.Helper()
	if _, err := p.Handlers()["complete"](context.Background(), h, []any{c}); err != nil {
		t.Fatalf("complete: %v", err)
	}
}

// What a source gathered is filtered again while the word is typed on, and
// gathered again when the word starts elsewhere, when the source is volatile
// or incomplete, and for a manual completion.
func TestCache(t *testing.T) {
	head := map[string]cmp.SourceOption{"_": {Matchers: []string{"matcherhead"}}}
	src := &countingSource{name: "s", items: words("alpha", "alps", "beta")}
	p := plugin(t, cmp.Config{Sources: []string{"s"}, SourceOptions: head}, src)
	h := uiHost()
	call(t, p, h, ctxMap(1, "a", ""))
	call(t, p, h, ctxMap(2, "alp", ""))
	if _, w := shown(t, h); !reflect.DeepEqual(w, []string{"alpha", "alps"}) {
		t.Errorf("alp: %q", w)
	}
	call(t, p, h, ctxMap(3, "alph", ""))
	if n := src.n.Load(); n != 1 {
		t.Errorf("gathered %d times for one word, want 1", n)
	}
	if _, w := shown(t, h); !reflect.DeepEqual(w, []string{"alpha"}) {
		t.Errorf("alph: %q", w)
	}
	// Another word: asked again.
	call(t, p, h, ctxMap(4, "x a", ""))
	if n := src.n.Load(); n != 2 {
		t.Errorf("another word: gathered %d times, want 2", n)
	}
	// Another line.
	c := ctxMap(5, "x al", "")
	c["lnum"] = int64(2)
	call(t, p, h, c)
	if n := src.n.Load(); n != 3 {
		t.Errorf("another line: gathered %d times, want 3", n)
	}
	// Manual: always asked.
	c = ctxMap(6, "x alp", "")
	c["lnum"], c["manual"] = int64(2), true
	call(t, p, h, c)
	if n := src.n.Load(); n != 4 {
		t.Errorf("manual: gathered %d times, want 4", n)
	}

	for _, tc := range []struct {
		name string
		opt  cmp.SourceOption
		src  *countingSource
		want int32
	}{
		{"isVolatile", cmp.SourceOption{IsVolatile: new(true)}, &countingSource{name: "s", items: words("alpha")}, 3},
		// volatilePattern: only when it matches ("ab" ends with "b").
		{"volatilePattern", cmp.SourceOption{IsVolatile: new(true), VolatilePattern: `b`}, &countingSource{name: "s", items: words("alpha")}, 2},
		{"incomplete", cmp.SourceOption{}, &countingSource{name: "s", items: words("alpha"), incomplete: true}, 3},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p := plugin(t, cmp.Config{Sources: []string{"s"}, SourceOptions: map[string]cmp.SourceOption{"s": tc.opt}}, tc.src)
			h := uiHost()
			for i, in := range []string{"a", "ab", "abc"} {
				call(t, p, h, ctxMap(int64(i+1), in, ""))
			}
			if n := tc.src.n.Load(); n != tc.want {
				t.Errorf("gathered %d times, want %d", n, tc.want)
			}
		})
	}
}

// update_items replaces what a source gathered and shows the list again.
func TestUpdateItems(t *testing.T) {
	head := map[string]cmp.SourceOption{"_": {Matchers: []string{"matcherhead"}}}
	src := &countingSource{name: "s", items: words("alpha")}
	p := plugin(t, cmp.Config{Sources: []string{"s"}, SourceOptions: head}, src)
	h := uiHost()
	call(t, p, h, ctxMap(1, "a", ""))
	c := ctxMap(2, "a", "")
	c["event"] = "Update"
	c["update"] = map[string]any{"name": "s", "items": []any{
		map[string]any{"word": "apple", "user_data": "stale"}, map[string]any{"word": "banana"},
	}}
	call(t, p, h, c)
	if _, w := shown(t, h); !reflect.DeepEqual(w, []string{"apple"}) {
		t.Errorf("after update: %q", w)
	}
	if n := src.n.Load(); n != 1 {
		t.Errorf("gathered %d times, want 1", n)
	}
}

func TestLengthOptions(t *testing.T) {
	src := fakeSource{name: "s", items: []string{"ab", "abcd", "abcdefgh"}}
	for _, tc := range []struct {
		name   string
		opt    cmp.SourceOption
		input  string
		manual bool
		want   []string
	}{
		{"keyword lengths", cmp.SourceOption{MinKeywordLength: new(3), MaxKeywordLength: new(5)}, "a", false, []string{"abcd"}},
		{"maxAutoCompleteLength", cmp.SourceOption{MaxAutoCompleteLength: new(2)}, "abc", false, nil},
		{"maxAutoCompleteLength, manual", cmp.SourceOption{MaxAutoCompleteLength: new(2)}, "abc", true, []string{"ab", "abcd", "abcdefgh"}},
		{"minManualCompleteLength", cmp.SourceOption{MinManualCompleteLength: new(2)}, "a", true, nil},
		{"maxManualCompleteLength", cmp.SourceOption{MaxManualCompleteLength: new(1)}, "ab", true, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			b := newBuilder()
			b.AddSource(src)
			b.PatchGlobal(cmp.Config{Sources: []string{"s"}, SourceOptions: map[string]cmp.SourceOption{"s": tc.opt}})
			h := uiHost()
			c := ctxMap(1, tc.input, "")
			c["manual"] = tc.manual
			complete(t, b, h, c)
			if _, w := shown(t, h); !reflect.DeepEqual(w, tc.want) {
				t.Errorf("words = %q, want %q", w, tc.want)
			}
		})
	}
}

// itemSource returns its items as they are.
type itemSource struct {
	name  string
	items []cmp.Item
}

func (s itemSource) Name() string { return s.name }
func (s itemSource) Gather(_ context.Context, _ sdk.Host, c *cmp.Context) (cmp.Gathered, error) {
	return cmp.Gathered{Start: c.KeywordStart(), Items: s.items}, nil
}

func TestMatcherKeyAndIgnoreCase(t *testing.T) {
	src := itemSource{name: "s", items: []cmp.Item{
		{Word: "x1", Abbr: "apple"}, {Word: "x2", Abbr: "banana", FilterText: "keep"}, {Word: "x3", Abbr: "Avocado"},
	}}
	b := newBuilder()
	b.AddSource(src)
	b.AddFilter(matcherhead.New(matcherhead.Params{}))
	b.PatchGlobal(cmp.Config{Sources: []string{"s"}, SourceOptions: map[string]cmp.SourceOption{
		"s": {Matchers: []string{"matcherhead"}, MatcherKey: "abbr", IgnoreCase: new(true), MinAutoCompleteLength: new(0)},
	}})
	h := uiHost()
	complete(t, b, h, ctxMap(1, "A", ""))
	// Matched on abbr, case ignored even with a capital typed.
	if _, w := shown(t, h); !reflect.DeepEqual(w, []string{"x1", "x3"}) {
		t.Errorf("words = %q", w)
	}
}

func TestDup(t *testing.T) {
	for _, tc := range []struct {
		dup  string
		want []string
		dups []int
	}{
		{"ignore", []string{"same", "a2", "b1"}, []int{0, 0, 0}},
		{"keep", []string{"same", "a2", "same", "b1"}, []int{0, 0, 1, 1}},
		{"force", []string{"a2", "same", "b1"}, []int{0, 0, 0}},
	} {
		t.Run(tc.dup, func(t *testing.T) {
			b := newBuilder()
			b.AddSource(itemSource{name: "a", items: words("same", "a2")})
			b.AddSource(itemSource{name: "b", items: words("same", "b1", "")})
			b.PatchGlobal(cmp.Config{Sources: []string{"a", "b"}, SourceOptions: map[string]cmp.SourceOption{"b": {Dup: tc.dup}}})
			h := uiHost()
			complete(t, b, h, ctxMap(1, "x", ""))
			var got []string
			var dups []int
			for _, c := range h.Calls() {
				if c.Fn == "goda_cmp#ui#native#show" {
					got, dups = nil, nil
					for _, it := range c.Args[0].(map[string]any)["items"].([]cmp.Item) {
						got = append(got, it.Word)
						dups = append(dups, it.Dup)
					}
				}
			}
			if !reflect.DeepEqual(got, tc.want) || !reflect.DeepEqual(dups, tc.dups) {
				t.Errorf("words %q dups %v, want %q %v", got, dups, tc.want, tc.dups)
			}
		})
	}
}

func TestEnabledIf(t *testing.T) {
	b := newBuilder()
	b.AddSource(fakeSource{name: "a", items: []string{"alpha"}})
	b.AddSource(fakeSource{name: "b", items: []string{"beta"}})
	b.PatchGlobal(cmp.Config{Sources: []string{"a", "b"}, SourceOptions: map[string]cmp.SourceOption{
		"a": {EnabledIf: "Off()"}, "b": {EnabledIf: "On()"},
	}})
	h := uiHost()
	h.Funcs["Off"] = func(...any) (any, error) { return int64(0), nil }
	h.Funcs["On"] = func(...any) (any, error) { return int64(1), nil }
	complete(t, b, h, ctxMap(1, "x", ""))
	if _, w := shown(t, h); !reflect.DeepEqual(w, []string{"beta"}) {
		t.Errorf("words = %q", w)
	}
}

// A plain source slower than hideTimeout is left out, and the popup hidden
// meanwhile; the others are shown.
func TestHideTimeout(t *testing.T) {
	slow := &countingSource{name: "slow", items: words("slowword"), delay: 300 * time.Millisecond}
	b := newBuilder()
	b.AddSource(slow)
	b.AddSource(fakeSource{name: "fast", items: []string{"fastword"}})
	b.PatchGlobal(cmp.Config{Sources: []string{"slow", "fast"}, SourceOptions: map[string]cmp.SourceOption{
		"slow": {HideTimeout: new(30)},
	}})
	h := uiHost()
	complete(t, b, h, ctxMap(1, "x", ""))
	var fns []string
	for _, c := range h.Calls() {
		if strings.HasPrefix(c.Fn, "goda_cmp#ui#native#") {
			fns = append(fns, c.Fn)
		}
	}
	if !reflect.DeepEqual(fns, []string{"goda_cmp#ui#native#hide", "goda_cmp#ui#native#show"}) {
		t.Errorf("ui calls = %v", fns)
	}
	if _, w := shown(t, h); !reflect.DeepEqual(w, []string{"fastword"}) {
		t.Errorf("words = %q", w)
	}
}

func TestReplaceSourceInputPattern(t *testing.T) {
	src := &countingSource{name: "s", items: words("bar")}
	b := newBuilder()
	b.AddSource(src)
	b.PatchGlobal(cmp.Config{Sources: []string{"s"}, SourceOptions: map[string]cmp.SourceOption{
		"s": {ReplaceSourceInputPattern: `^\s+`},
	}})
	h := uiHost()
	complete(t, b, h, ctxMap(1, "  foo ba", ""))
	if !reflect.DeepEqual(src.inputs, []string{"foo ba"}) {
		t.Errorf("the source saw %q", src.inputs)
	}
	// Its start (4 in "foo ba") is 6 in the real input: column 7.
	if col, w := shown(t, h); col != 7 || !reflect.DeepEqual(w, []string{"bar"}) {
		t.Errorf("col %d words %q", col, w)
	}
}

func TestDynamicFiltersAndConfig(t *testing.T) {
	b := newBuilder()
	b.AddSource(fakeSource{name: "a", items: []string{"alpha", "beta"}})
	b.AddSource(fakeSource{name: "b", items: []string{"bravo"}})
	b.AddFilter(matcherhead.New(matcherhead.Params{}))
	var seen []string
	b.PatchGlobal(cmp.Config{
		Sources: []string{"a"},
		SourceOptions: map[string]cmp.SourceOption{"_": {
			MinAutoCompleteLength: new(0),
			DynamicFilters: func(_ context.Context, _ sdk.Host, a cmp.DynamicFiltersArgs) (cmp.Filters, error) {
				seen = append(seen, a.Source+":"+a.Complete)
				return cmp.Filters{Matchers: []string{"matcherhead"}}, nil
			},
		}},
		// In a comment, the other source.
		DynamicConfig: func(_ context.Context, _ sdk.Host, a cmp.DynamicConfigArgs) (cmp.Config, error) {
			if strings.HasPrefix(a.Context.Input, "//") {
				return cmp.Config{Sources: []string{"b"}}, nil
			}
			return cmp.Config{}, nil
		},
	})
	h := uiHost()
	complete(t, b, h, ctxMap(1, "b", ""))
	if _, w := shown(t, h); !reflect.DeepEqual(w, []string{"beta"}) {
		t.Errorf("words = %q", w)
	}
	complete(t, b, h, ctxMap(1, "// b", ""))
	if _, w := shown(t, h); !reflect.DeepEqual(w, []string{"bravo"}) {
		t.Errorf("in a comment: %q", w)
	}
	if !reflect.DeepEqual(seen, []string{"a:b", "b:b"}) {
		t.Errorf("dynamicFilters saw %q", seen)
	}
}

// A source with parameters that can be set from Vim script.
type paramSource struct {
	p struct {
		Word  string `json:"word"`
		Times int    `json:"times"`
	}
}

func (paramSource) Name() string { return "param" }
func (s paramSource) Gather(_ context.Context, _ sdk.Host, c *cmp.Context) (cmp.Gathered, error) {
	return cmp.Gathered{Start: c.KeywordStart(), Items: words(strings.Repeat(s.p.Word, s.p.Times))}, nil
}
func (s paramSource) WithParams(p cmp.Params) (any, error) {
	np, err := cmp.PatchParams(s.p, p)
	if err != nil {
		return nil, err
	}
	return paramSource{p: np}, nil
}

func TestParamsAndAliases(t *testing.T) {
	src := paramSource{}
	src.p.Word, src.p.Times = "ab", 1
	b := newBuilder()
	b.AddSource(src)
	b.AddSource(fakeSource{name: "plain", items: []string{"x"}})
	b.PatchGlobal(cmp.Config{Sources: []string{"param"}})
	p, err := b.Plugin()
	if err != nil {
		t.Fatal(err)
	}
	h := uiHost()
	run := func(patches map[string]any) []string {
		t.Helper()
		call(t, p, h, withPatches(ctxMap(1, "a", ""), patches))
		_, w := shown(t, h)
		return w
	}
	if w := run(nil); !reflect.DeepEqual(w, []string{"ab"}) {
		t.Errorf("Go params: %q", w)
	}
	// sourceParams merge by key: "times" from the global patch, "word"
	// from the buffer.
	w := run(map[string]any{
		"global": map[string]any{"sourceParams": map[string]any{"param": map[string]any{"times": 3}}},
		"buffer": map[string]any{"sourceParams": map[string]any{"param": map[string]any{"word": "c"}}},
	})
	if !reflect.DeepEqual(w, []string{"ccc"}) {
		t.Errorf("params from Vim: %q", w)
	}
	// An alias with params of its own, next to the source.
	w = run(map[string]any{
		"aliases": map[string]any{"source": map[string]any{"param2": "param"}},
		"global": map[string]any{
			"sources":      []any{"param", "param2"},
			"sourceParams": map[string]any{"param2": map[string]any{"word": "az"}},
		},
	})
	if !reflect.DeepEqual(w, []string{"ab", "az"}) {
		t.Errorf("alias: %q", w)
	}

	// Errors, through validate.
	for _, tc := range []struct {
		config, aliases map[string]any
		want            string
	}{
		{map[string]any{"sourceParams": map[string]any{"param": map[string]any{"nope": 1}}}, nil, `unknown field "nope"`},
		{map[string]any{"sourceParams": map[string]any{"plain": map[string]any{"x": 1}}}, nil, "takes no params"},
		{map[string]any{"sources": []any{"other"}}, map[string]any{"source": map[string]any{"other": "plain"}}, ""},
		{map[string]any{}, map[string]any{"source": map[string]any{"bad": "missing"}}, `unknown source "missing"`},
	} {
		args := []any{tc.config}
		if tc.aliases != nil {
			args = append(args, tc.aliases)
		}
		_, err := p.Handlers()["validate"](context.Background(), nil, args)
		if tc.want == "" {
			if err != nil {
				t.Errorf("%v: %v", tc.config, err)
			}
		} else if err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%v: err = %v, want %q", tc.config, err, tc.want)
		}
	}
}

func TestContextFiletypeScope(t *testing.T) {
	b := newBuilder()
	b.PatchFiletype("go", cmp.Config{ContextFiletype: "treesitter"})
	if _, err := b.Plugin(); err == nil || !strings.Contains(err.Error(), "contextFiletype") {
		t.Errorf("filetype contextFiletype: err = %v", err)
	}
	b = newBuilder()
	b.PatchGlobal(cmp.Config{ContextFiletype: "treesitter"})
	p, err := b.Plugin()
	if err != nil {
		t.Fatal(err)
	}
	opts, err := p.Handlers()["options"](context.Background(), nil, []any{"", nil})
	if err != nil || opts.(map[string]any)["contextFiletype"] != "treesitter" {
		t.Errorf("options = %v, %v", opts, err)
	}
	_, err = p.Handlers()["validate"](context.Background(), nil, []any{map[string]any{"contextFiletype": "none"}, nil, "filetype"})
	if err == nil {
		t.Error("contextFiletype in a filetype patch must be an error")
	}
	if _, err := p.Handlers()["validate"](context.Background(), nil, []any{map[string]any{"contextFiletype": "none"}, nil, "buffer"}); err != nil {
		t.Errorf("buffer: %v", err)
	}
}

// After goda_cmp#complete({options}), typing on narrows with the same
// sources while the list is open.
func TestManualContinues(t *testing.T) {
	b := newBuilder()
	b.AddSource(fakeSource{name: "a", items: []string{"alpha"}})
	b.AddSource(fakeSource{name: "b", items: []string{"bravo"}})
	b.PatchGlobal(cmp.Config{Sources: []string{"a"}, SourceOptions: map[string]cmp.SourceOption{"_": {MinAutoCompleteLength: new(5)}}})
	p, err := b.Plugin()
	if err != nil {
		t.Fatal(err)
	}
	h := uiHost()
	c := ctxMap(1, "x", "")
	c["manual"], c["event"] = true, "Manual"
	c["options"] = map[string]any{"sources": []any{"b"}}
	call(t, p, h, c)
	if _, w := shown(t, h); !reflect.DeepEqual(w, []string{"bravo"}) {
		t.Fatalf("manual: %q", w)
	}
	// "xy" is shorter than minAutoCompleteLength, but continues the manual
	// completion with source b.
	c = ctxMap(2, "xy", "")
	c["event"] = "TextChangedI"
	call(t, p, h, c)
	if _, w := shown(t, h); !reflect.DeepEqual(w, []string{"bravo"}) {
		t.Errorf("continued: %q", w)
	}
	// After hide, it is automatic again: too short, nothing.
	if _, err := p.Handlers()["hide"](context.Background(), h, nil); err != nil {
		t.Fatal(err)
	}
	n := len(h.Calls())
	c = ctxMap(3, "xyz", "")
	c["event"] = "TextChangedI"
	call(t, p, h, c)
	for _, cl := range h.Calls()[n:] {
		if cl.Fn == "goda_cmp#ui#native#show" {
			t.Errorf("shown after hide: %v", cl.Args)
		}
	}
}

func TestAutoCompleteEventsAndPreviewOption(t *testing.T) {
	b := newBuilder()
	b.AddSource(previewSource{fakeSource{name: "p", items: []string{"alpha"}}})
	b.PatchGlobal(cmp.Config{
		Sources:            []string{"p"},
		AutoCompleteEvents: []string{"TextChangedI"},
		SourceOptions:      map[string]cmp.SourceOption{"p": {Preview: new(false)}},
	})
	p, err := b.Plugin()
	if err != nil {
		t.Fatal(err)
	}
	h := uiHost()
	c := ctxMap(1, "a", "")
	c["event"] = "InsertEnter" // not in autoCompleteEvents
	call(t, p, h, c)
	if len(h.Calls()) != 0 {
		t.Errorf("InsertEnter completed: %v", h.Calls())
	}
	c = ctxMap(2, "a", "")
	c["event"] = "TextChangedI"
	call(t, p, h, c)
	ref := refsOf(h)["alpha"]
	got, err := p.Handlers()["preview"](context.Background(), h, []any{ref})
	if err != nil || !reflect.DeepEqual(got, map[string]any{"kind": "empty"}) {
		t.Errorf("preview with preview off = %v, %v", got, err)
	}
	// visible: the native UI asks pumvisible().
	h.Funcs["pumvisible"] = func(...any) (any, error) { return int64(1), nil }
	v, err := p.Handlers()["visible"](context.Background(), h, nil)
	if err != nil || v != true {
		t.Errorf("visible = %v, %v", v, err)
	}
}

var _ = cmptest.Host{}

package cmp_test

import (
	"context"
	"errors"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/Shougo/goda-cmp.vim/cmp"
	"github.com/Shougo/goda-cmp.vim/cmp/cmptest"
	"github.com/Shougo/goda.vim/host"
	"github.com/Shougo/goda.vim/sdk"
)

type fakeSource struct {
	name  string
	start func(c *cmp.Context) int
	items []string
	err   error
	panic bool
	block bool
}

func (s fakeSource) Name() string { return s.name }
func (s fakeSource) Gather(ctx context.Context, _ sdk.Host, c *cmp.Context) (cmp.Gathered, error) {
	if s.panic {
		panic("boom")
	}
	if s.block {
		<-ctx.Done()
		return cmp.Gathered{}, ctx.Err()
	}
	if s.err != nil {
		return cmp.Gathered{}, s.err
	}
	start := c.KeywordStart()
	if s.start != nil {
		start = s.start(c)
	}
	var items []cmp.Item
	for _, w := range s.items {
		items = append(items, cmp.Item{Word: w})
	}
	return cmp.Gathered{Start: start, Items: items}, nil
}

// newBuilder is cmp.New with two test UIs, "insert" (insert mode only)
// and "every" (every mode, a cmp.Skipper), "insert" chosen.
func newBuilder() *cmp.Builder {
	return cmp.New().AddUI(cmptest.NewUI("insert")).AddUI(cmptest.NewUI("every", "i", "c", "t").WithSkip()).PatchGlobal(cmp.Config{UI: "insert"})
}

// uiHost fakes the Vim side of the test UIs and remembers what was shown.
func uiHost(lines ...string) *cmptest.Host {
	h := cmptest.Lines(lines...)
	h.Funcs["goda_cmp_testui#insert#show"] = func(...any) (any, error) { return nil, nil }
	h.Funcs["goda_cmp_testui#insert#hide"] = func(...any) (any, error) { return nil, nil }
	h.Funcs["goda_cmp_testui#every#skip"] = func(...any) (any, error) { return false, nil }
	return h
}

func ctxMap(seq int64, input, ft string) map[string]any {
	return map[string]any{
		"seq": seq, "tick": int64(1), "bufnr": int64(1), "lnum": int64(1),
		"col": int64(len(input) + 1), "input": input, "filetype": ft,
	}
}

func complete(t *testing.T, b *cmp.Builder, h sdk.Host, c map[string]any) {
	t.Helper()
	p, err := b.Plugin()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := p.Handlers()["complete"](context.Background(), h, []any{c}); err != nil {
		t.Fatalf("complete: %v", err)
	}
}

// shown returns the words of the last Show, and its column.
func shown(t *testing.T, h *cmptest.Host) (col int, words []string) {
	t.Helper()
	calls := h.Calls()
	for i := len(calls) - 1; i >= 0; i-- {
		if calls[i].Fn == "goda_cmp_testui#insert#hide" {
			return 0, nil
		}
		if calls[i].Fn == "goda_cmp_testui#insert#show" {
			m := calls[i].Args[0].(map[string]any)
			for _, it := range m["items"].([]cmp.Item) {
				words = append(words, it.Word)
			}
			return m["col"].(int), words
		}
	}
	t.Fatal("neither show nor hide was called")
	return
}

func TestPipeline(t *testing.T) {
	b := newBuilder()
	b.AddSource(cmptest.NewWords(cmptest.WordsParams{}))
	b.AddFilter(cmptest.NewHead())
	b.AddFilter(lengthSorter{})
	b.PatchGlobal(cmp.Config{
		Sources: []string{"words"},
		SourceOptions: map[string]cmp.SourceOption{
			"_": {Matchers: []string{"head"}, Sorters: []string{"length"}},
		},
	})
	h := uiHost("func fooBarBaz() {", "\tfoo := fooBar + other", "\tfo")
	c := ctxMap(1, "\tfo", "go")
	c["lnum"] = int64(3)
	complete(t, b, h, c)
	col, words := shown(t, h)
	// "fo" itself is not offered; matched by its head; shorter first.
	if want := []string{"foo", "fooBar", "fooBarBaz"}; !reflect.DeepEqual(words, want) {
		t.Errorf("words = %q, want %q", words, want)
	}
	if col != 2 { // after the tab
		t.Errorf("col = %d, want 2", col)
	}
}

// lengthSorter is a sorter for tests: shorter words first.
type lengthSorter struct{}

func (lengthSorter) Name() string { return "length" }

func (lengthSorter) Filter(_ context.Context, in cmp.FilterInput) ([]cmp.Item, error) {
	out := slices.Clone(in.Items)
	slices.SortStableFunc(out, func(a, b cmp.Item) int { return len(a.Word) - len(b.Word) })
	return out, nil
}

func TestMinAutoCompleteLengthAndManual(t *testing.T) {
	b := newBuilder()
	b.AddSource(fakeSource{name: "s", items: []string{"alpha"}})
	b.PatchGlobal(cmp.Config{
		Sources:       []string{"s"},
		SourceOptions: map[string]cmp.SourceOption{"_": {MinAutoCompleteLength: new(3)}},
	})
	h := uiHost()
	complete(t, b, h, ctxMap(1, "al", "")) // too short
	if _, w := shown(t, h); len(w) != 0 {
		t.Errorf("short input produced %q", w)
	}
	c := ctxMap(2, "al", "")
	c["manual"] = true
	complete(t, b, h, c)
	if _, w := shown(t, h); !reflect.DeepEqual(w, []string{"alpha"}) {
		t.Errorf("manual completion produced %q", w)
	}
}

// positionedSource says where its text starts: after the last "▽".
type positionedSource struct{ fakeSource }

func (positionedSource) CompletePosition(_ context.Context, _ sdk.Host, c *cmp.Context) int {
	if i := strings.LastIndex(c.Input, "▽"); i >= 0 {
		return i
	}
	return -1
}

// A CompletePositioner decides the completed lengths, not the keyword.
func TestCompletePositioner(t *testing.T) {
	b := newBuilder()
	b.AddSource(positionedSource{fakeSource{name: "s", items: []string{"漢字"},
		start: func(c *cmp.Context) int { return strings.LastIndex(c.Input, "▽") }}})
	b.PatchGlobal(cmp.Config{
		Sources: []string{"s"},
		SourceOptions: map[string]cmp.SourceOption{
			// No keyword at all before the cursor, nor of 3 characters.
			"_": {KeywordPattern: "[ァ-ヮ]+", MinAutoCompleteLength: new(3)},
		},
	})
	for i, c := range []struct {
		input string
		want  []string
	}{
		{"x▽かん", []string{"漢字"}},
		{"x▽か", nil}, // shorter than 3: "▽か"
		{"xかん", nil}, // nothing to complete
	} {
		h := uiHost()
		complete(t, b, h, ctxMap(int64(i+1), c.input, ""))
		if _, w := shown(t, h); !reflect.DeepEqual(w, c.want) {
			t.Errorf("%q: words = %q, want %q", c.input, w, c.want)
		}
	}
}

// forceCompletionPattern overrides MinAutoCompleteLength (as in ddc.vim),
// also for an empty word: "x." asks a source that completes after the dot.
func TestForceCompletionPattern(t *testing.T) {
	b := newBuilder()
	b.AddSource(fakeSource{name: "s", items: []string{"member"}})
	b.AddSource(fakeSource{name: "t", items: []string{"other"}})
	b.PatchGlobal(cmp.Config{
		Sources: []string{"s", "t"},
		SourceOptions: map[string]cmp.SourceOption{
			"_": {MinAutoCompleteLength: new(2)},
			"s": {ForceCompletionPattern: `\.\w*`},
		},
	})
	for i, c := range []struct {
		input string
		want  []string
	}{
		{"x.", []string{"member"}},  // forced, empty word
		{"x.m", []string{"member"}}, // forced, word shorter than 2
		{"x m", nil},                // neither forced nor long enough
		{"x me", []string{"member", "other"}},
	} {
		h := uiHost()
		complete(t, b, h, ctxMap(int64(i+1), c.input, ""))
		if _, w := shown(t, h); !reflect.DeepEqual(w, c.want) {
			t.Errorf("%q: words = %q, want %q", c.input, w, c.want)
		}
	}
	if err := newBuilder().AddSource(fakeSource{name: "s"}).PatchGlobal(cmp.Config{
		SourceOptions: map[string]cmp.SourceOption{"s": {ForceCompletionPattern: "(x"}},
	}).Validate(); err == nil || !strings.Contains(err.Error(), "forceCompletionPattern") {
		t.Errorf("invalid pattern: %v", err)
	}
}

func TestBrokenSourcesDoNotHurtOthers(t *testing.T) {
	b := newBuilder()
	b.AddSource(fakeSource{name: "good", items: []string{"one"}})
	b.AddSource(fakeSource{name: "bad", err: errors.New("nope")})
	b.AddSource(fakeSource{name: "panics", panic: true})
	b.AddSource(fakeSource{name: "hangs", block: true})
	b.PatchGlobal(cmp.Config{
		Sources:       []string{"bad", "panics", "hangs", "good"},
		SourceTimeout: new(100),
	})
	h := uiHost()
	start := time.Now()
	complete(t, b, h, ctxMap(1, "o", ""))
	if _, w := shown(t, h); !reflect.DeepEqual(w, []string{"one"}) {
		t.Errorf("words = %q", w)
	}
	if d := time.Since(start); d > 2*time.Second {
		t.Errorf("a hanging source held the request for %v", d)
	}
}

// A source that answered nothing (here: failed) must not take part in the
// alignment: its slot has no start.
func TestFailedSourceAndLaterStart(t *testing.T) {
	b := newBuilder()
	b.AddSource(fakeSource{name: "bad", err: errors.New("nope")})
	b.AddSource(fakeSource{name: "good", items: []string{"bar"}})
	b.PatchGlobal(cmp.Config{Sources: []string{"bad", "good"}})
	h := uiHost()
	complete(t, b, h, ctxMap(1, "foo ba", ""))
	if col, w := shown(t, h); col != 5 || !reflect.DeepEqual(w, []string{"bar"}) {
		t.Errorf("col=%d words=%q", col, w)
	}
}

func TestSourcesWithDifferentStartsAreAligned(t *testing.T) {
	b := newBuilder()
	b.AddSource(fakeSource{name: "word", items: []string{"world"}})
	b.AddSource(fakeSource{
		name:  "path",
		items: []string{"wor/ld.txt"},
		start: func(c *cmp.Context) int { return strings.LastIndex(c.Input, "/") + 1 },
	})
	b.PatchGlobal(cmp.Config{Sources: []string{"word", "path"}})
	h := uiHost()
	complete(t, b, h, ctxMap(1, "a/wor", ""))
	col, w := shown(t, h)
	// word starts at offset 2, path also at 2 here: equal; check no shift.
	if col != 3 || !reflect.DeepEqual(w, []string{"world", "wor/ld.txt"}) {
		t.Errorf("col=%d words=%q", col, w)
	}

	b2 := newBuilder()
	b2.AddSource(fakeSource{name: "word", items: []string{"world"}})
	b2.AddSource(fakeSource{
		name:  "path",
		items: []string{"x.txt"},
		start: func(c *cmp.Context) int { return len(c.Input) }, // starts later
	})
	b2.PatchGlobal(cmp.Config{
		Sources:       []string{"word", "path"},
		SourceOptions: map[string]cmp.SourceOption{"_": {MinAutoCompleteLength: new(0)}},
	})
	h2 := uiHost()
	complete(t, b2, h2, ctxMap(1, "wor", ""))
	col, w = shown(t, h2)
	if col != 1 || !reflect.DeepEqual(w, []string{"world", "worx.txt"}) {
		t.Errorf("col=%d words=%q, want 1 and the later source prefixed", col, w)
	}
}

func TestFiletypePatch(t *testing.T) {
	b := newBuilder()
	b.AddSource(fakeSource{name: "a", items: []string{"from_a"}})
	b.AddSource(fakeSource{name: "b", items: []string{"from_b"}})
	b.PatchGlobal(cmp.Config{Sources: []string{"a"}})
	b.PatchFiletype("go", cmp.Config{Sources: []string{"b"}})
	b.PatchFiletype("cpp", cmp.Config{Sources: []string{"a", "b"}})

	for ft, want := range map[string][]string{
		"":            {"from_a"},
		"go":          {"from_b"},
		"cpp.doxygen": {"from_a", "from_b"},
		"rust":        {"from_a"},
	} {
		h := uiHost()
		complete(t, b, h, ctxMap(1, "f", ft))
		if _, w := shown(t, h); !reflect.DeepEqual(w, want) {
			t.Errorf("filetype %q: %q, want %q", ft, w, want)
		}
	}
}

func TestSupersededRequestIsDropped(t *testing.T) {
	b := newBuilder()
	b.AddSource(fakeSource{name: "a", items: []string{"x"}})
	b.PatchGlobal(cmp.Config{Sources: []string{"a"}})
	h := uiHost()
	p, err := b.Plugin() // one engine: it remembers the newest request
	if err != nil {
		t.Fatal(err)
	}
	serve := func(seq int64) {
		if _, err := p.Handlers()["complete"](context.Background(), h, []any{ctxMap(seq, "x", "")}); err != nil {
			t.Fatal(err)
		}
	}
	serve(5)
	n := len(h.Calls())
	serve(4) // older than the one already served
	if len(h.Calls()) != n {
		t.Error("a stale request reached the UI")
	}
}

func TestUIChoice(t *testing.T) {
	b := newBuilder()
	b.AddSource(fakeSource{name: "a", items: []string{"x"}})
	b.PatchGlobal(cmp.Config{Sources: []string{"a"}, UI: "every"})
	h := uiHost()
	h.Funcs["goda_cmp_testui#every#show"] = func(...any) (any, error) { return nil, nil }
	complete(t, b, h, ctxMap(1, "x", ""))
	var got []string
	for _, c := range h.Calls() {
		if c.Fn != "goda_cmp#_items" { // the core's own record of the list
			got = append(got, c.Fn)
		}
	}
	// The UI is asked first whether the change was its own.
	if !reflect.DeepEqual(got, []string{"goda_cmp_testui#every#skip", "goda_cmp_testui#every#show"}) {
		t.Errorf("calls = %v", got)
	}
}

// A UI that says the change was its own (cmp.Skipper) drops automatic
// requests, not manual ones.
func TestSkipCompletion(t *testing.T) {
	b := newBuilder()
	b.AddSource(fakeSource{name: "a", items: []string{"xyz"}})
	b.PatchGlobal(cmp.Config{Sources: []string{"a"}, UI: "every"})
	h := uiHost()
	h.Funcs["goda_cmp_testui#every#show"] = func(...any) (any, error) { return nil, nil }
	// The UI says the change was its own (an item it inserted) while
	// own is set.
	own := true
	h.Funcs["goda_cmp_testui#every#skip"] = func(...any) (any, error) { return own, nil }
	shows := func() int {
		n := 0
		for _, c := range h.Calls() {
			if c.Fn == "goda_cmp_testui#every#show" {
				n++
			}
		}
		return n
	}

	c := ctxMap(1, "x", "")
	c["event"] = "TextChangedP"
	complete(t, b, h, c)
	if n := shows(); n != 0 {
		t.Fatalf("shown %d times while the user moves through the popup", n)
	}
	c = ctxMap(2, "x", "")
	c["event"] = "TextChangedP"
	c["manual"] = true // asked for: never skipped
	complete(t, b, h, c)
	own = false
	c = ctxMap(3, "x", "")
	c["event"] = "TextChangedI" // typing
	complete(t, b, h, c)
	if n := shows(); n != 2 {
		t.Errorf("shown %d times, want 2", n)
	}
	// The UI is not asked about the manual request.
	n := 0
	for _, cl := range h.Calls() {
		if cl.Fn == "goda_cmp_testui#every#skip" {
			n++
		}
	}
	if n != 2 {
		t.Errorf("skip asked %d times, want 2", n)
	}
}

// After a source resolved an item, the engine hands it to the UI that showed
// it (cmp.InfoUpdater); no Vim script of the core is involved.
func TestUpdateInfo(t *testing.T) {
	b := newBuilder()
	b.AddSource(resolvingSource{fakeSource{name: "a", items: []string{"xyz"}}})
	b.PatchGlobal(cmp.Config{Sources: []string{"a"}})
	p, err := b.Plugin()
	if err != nil {
		t.Fatal(err)
	}
	h := uiHost()
	var got map[string]any
	h.Funcs["goda_cmp_testui#insert#update_info"] = func(a ...any) (any, error) {
		got = a[0].(map[string]any)
		return nil, nil
	}
	ctx := context.Background()
	if _, err := p.Handlers()["complete"](ctx, h, []any{ctxMap(1, "x", "")}); err != nil {
		t.Fatal(err)
	}
	if _, err := p.Handlers()["select"](ctx, h, []any{"1:0"}); err != nil {
		t.Fatal(err)
	}
	if got["ref"] != "1:0" || got["info"] != "resolved xyz" {
		t.Errorf("update_info got %v", got)
	}
	// The receivers are for the Vim script of UIs, not for the engine.
	for _, c := range h.Calls() {
		if slices.Contains([]string{"goda_cmp#_fresh", "goda_cmp#_select", "goda_cmp#_done"}, c.Fn) {
			t.Errorf("the engine called a receiver: %s", c.Fn)
		}
	}
}

type resolvingSource struct{ fakeSource }

func (resolvingSource) Resolve(_ context.Context, _ sdk.Host, a cmp.DoneArgs) (cmp.Item, error) {
	it := a.Item
	it.Info = "resolved " + it.Word
	return it, nil
}

func TestAutoCompleteOff(t *testing.T) {
	b := newBuilder()
	b.AddSource(fakeSource{name: "a", items: []string{"x"}})
	b.PatchGlobal(cmp.Config{Sources: []string{"a"}, AutoComplete: new(false)})
	h := uiHost()
	complete(t, b, h, ctxMap(1, "x", ""))
	if len(h.Calls()) != 0 {
		t.Error("completion ran with AutoComplete off")
	}
	c := ctxMap(2, "x", "")
	c["manual"] = true
	complete(t, b, h, c)
	if len(h.Calls()) == 0 {
		t.Error("manual completion must work with AutoComplete off")
	}
}

func TestValidate(t *testing.T) {
	b := newBuilder()
	b.AddSource(fakeSource{name: "a"})
	b.PatchGlobal(cmp.Config{
		UI:      "nope",
		Sources: []string{"a", "missing"},
		SourceOptions: map[string]cmp.SourceOption{
			"ghost": {},
			"a":     {Matchers: []string{"no_such_filter"}},
		},
	})
	b.PatchFiletype("go", cmp.Config{PostFilters: []string{"also_missing"}})
	_, err := b.Plugin()
	if err == nil {
		t.Fatal("expected an error")
	}
	for _, want := range []string{`unknown ui "nope"`, `unknown source "missing"`, `unknown source "ghost"`,
		`unknown filter "no_such_filter"`, `unknown filter "also_missing"`} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error lacks %q:\n%v", want, err)
		}
	}
}

func TestDuplicateRegistration(t *testing.T) {
	b := newBuilder()
	b.AddSource(fakeSource{name: "a"})
	b.AddSource(fakeSource{name: "a"})
	if _, err := b.Plugin(); err == nil || !strings.Contains(err.Error(), "twice") {
		t.Errorf("err = %v", err)
	}
}

func TestMergeSemantics(t *testing.T) {
	b := newBuilder()
	b.AddSource(fakeSource{name: "a"})
	b.AddFilter(cmptest.NewHead())
	b.AddFilter(lengthSorter{})
	b.PatchGlobal(cmp.Config{
		Sources: []string{"a"},
		SourceOptions: map[string]cmp.SourceOption{
			"a": {Mark: "A", Matchers: []string{"head"}, MaxItems: new(5)},
		},
	})
	// A second patch keeps Mark and MaxItems, replaces Matchers, adds Sorters.
	b.PatchGlobal(cmp.Config{SourceOptions: map[string]cmp.SourceOption{
		"a": {Matchers: []string{}, Sorters: []string{"length"}},
	}})
	p, err := b.Plugin()
	if err != nil {
		t.Fatal(err)
	}
	got, err := p.Handlers()["options"](context.Background(), nil, []any{""})
	if err != nil {
		t.Fatal(err)
	}
	m := got.(map[string]any)
	if m["ui"] != "insert" || m["delay"] != 0 || m["autoComplete"] != true {
		t.Errorf("options = %v", m)
	}
	// The effective source option is visible through behaviour: Mark is used.
	h := uiHost()
	b.AddSource(fakeSource{name: "z", items: []string{"zed"}})
	b.PatchGlobal(cmp.Config{Sources: []string{"z"}, SourceOptions: map[string]cmp.SourceOption{"z": {Mark: "Z"}}})
	complete(t, b, h, ctxMap(1, "z", ""))
	for _, c := range h.Calls() {
		if c.Fn == "goda_cmp_testui#insert#show" {
			items := c.Args[0].(map[string]any)["items"].([]cmp.Item)
			if items[0].Menu != "Z" {
				t.Errorf("menu = %q, want the mark Z", items[0].Menu)
			}
		}
	}
}

func TestKeywordStart(t *testing.T) {
	for in, want := range map[string]int{"": 0, "foo": 0, "a foo": 2, "a.": 2, "日本 語": 7, "x_1": 0} {
		if got := cmp.KeywordStart(in); got != want {
			t.Errorf("KeywordStart(%q) = %d, want %d", in, got, want)
		}
	}
}

// The plugin must be accepted by goda.vim's own registration rules.
func TestPluginFitsTheHost(t *testing.T) {
	p, err := newBuilder().Plugin()
	if err != nil {
		t.Fatal(err)
	}
	table, err := host.Table(p)
	if err != nil {
		t.Fatal(err)
	}
	for _, m := range []string{"cmp.complete", "cmp.hide", "cmp.options", "cmp.sources"} {
		if table[m] == nil {
			t.Errorf("missing %s", m)
		}
	}
}

// On the command line the sources are CmdlineSources of the command-line
// type, shown by a UI that works there.
func TestCmdlineSources(t *testing.T) {
	b := newBuilder()
	b.AddSource(fakeSource{name: "buf", items: []string{"buffer_word"}})
	b.AddSource(fakeSource{name: "cmd", items: []string{"command"}})
	b.PatchGlobal(cmp.Config{
		UI:             "every",
		Sources:        []string{"buf"},
		CmdlineSources: map[string][]string{":": {"cmd"}},
	})
	h := uiHost()
	h.Funcs["goda_cmp_testui#every#show"] = func(...any) (any, error) { return nil, nil }
	shown := func() []string {
		var words []string
		for _, c := range h.Calls() {
			if c.Fn == "goda_cmp_testui#every#show" {
				words = nil
				for _, it := range c.Args[0].(map[string]any)["items"].([]cmp.Item) {
					words = append(words, it.Word)
				}
			}
		}
		return words
	}
	c := ctxMap(1, "c", "")
	c["mode"], c["cmdtype"] = "c", ":"
	complete(t, b, h, c)
	if w := shown(); !reflect.DeepEqual(w, []string{"command"}) {
		t.Errorf(": shows %q", w)
	}
	c = ctxMap(2, "b", "")
	complete(t, b, h, c)
	if w := shown(); !reflect.DeepEqual(w, []string{"buffer_word"}) {
		t.Errorf("insert mode shows %q", w)
	}
	// A command-line type without sources: nothing.
	n := len(h.Calls())
	c = ctxMap(3, "c", "")
	c["mode"], c["cmdtype"] = "c", "/"
	complete(t, b, h, c)
	for _, call := range h.Calls()[n:] {
		if call.Fn == "goda_cmp_testui#every#show" {
			t.Errorf("/ showed something")
		}
	}
}

func TestCmdlineNeedsModesUI(t *testing.T) {
	b := newBuilder() // UI "insert", which cannot show on the command line
	b.AddSource(fakeSource{name: "cmd"})
	b.PatchGlobal(cmp.Config{CmdlineSources: map[string][]string{":": {"cmd"}}})
	if err := b.Validate(); err == nil || !strings.Contains(err.Error(), `ui "insert" cannot show candidates on the command line`) {
		t.Errorf("insert: err = %v", err)
	}
	b.PatchGlobal(cmp.Config{UI: "every"})
	if err := b.Validate(); err != nil {
		t.Errorf("every: %v", err)
	}
	b.PatchGlobal(cmp.Config{CmdlineSources: map[string][]string{"/": {"nope"}}})
	if err := b.Validate(); err == nil || !strings.Contains(err.Error(), `unknown source "nope" in cmdlineSources["/"]`) {
		t.Errorf("unknown source: err = %v", err)
	}
}

// ModesUI: a UI without it shows in insert mode only, one with it in the
// modes it lists.
func TestModesUI(t *testing.T) {
	for _, tc := range []struct {
		ui, mode string
		ok       bool
	}{
		{"insert", "i", true},
		{"insert", "t", false},
		{"every", "c", true},
		{"every", "t", true},
	} {
		b := newBuilder()
		b.AddSource(fakeSource{name: "a", items: []string{"x"}})
		b.PatchGlobal(cmp.Config{Sources: []string{"a"}, UI: tc.ui})
		h := uiHost()
		h.Funcs["goda_cmp_testui#every#show"] = func(...any) (any, error) { return nil, nil }
		c := ctxMap(1, "x", "")
		c["mode"] = tc.mode
		err := completeErr(t, b, h, c)
		if tc.ok != (err == nil) {
			t.Errorf("%s in mode %q: err = %v", tc.ui, tc.mode, err)
		}
	}
}

// input() is completed without goda_cmp#enable_cmdline(): with no sources for
// its type, it is no error, even with a UI that cannot show there.
func TestCmdlineTypeWithoutSources(t *testing.T) {
	b := newBuilder()
	b.AddSource(fakeSource{name: "a", items: []string{"x"}})
	b.PatchGlobal(cmp.Config{Sources: []string{"a"}, UI: "insert"})
	h := uiHost()
	c := ctxMap(1, "x", "")
	c["mode"], c["cmdtype"] = "c", "@"
	if err := completeErr(t, b, h, c); err != nil {
		t.Errorf("input() without sources: %v", err)
	}
	if len(h.Calls()) != 0 {
		t.Errorf("calls: %v", h.Calls())
	}
}

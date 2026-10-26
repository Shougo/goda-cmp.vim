package cmp_test

import (
	"context"
	"reflect"
	"strings"
	"testing"

	"github.com/Shougo/goda-cmp.vim/cmp"
	"github.com/Shougo/goda-cmp.vim/cmp/cmptest"
	"github.com/Shougo/goda.vim/sdk"
)

// completeErr is complete() for tests that expect an error.
func completeErr(t *testing.T, b *cmp.Builder, h sdk.Host, c map[string]any) error {
	t.Helper()
	p, err := b.Plugin()
	if err != nil {
		t.Fatal(err)
	}
	_, err = p.Handlers()["complete"](context.Background(), h, []any{c})
	return err
}

func withPatches(c map[string]any, patches map[string]any) map[string]any {
	c["patches"] = patches
	return c
}

func twoSources() *cmp.Builder {
	b := newBuilder()
	b.AddSource(fakeSource{name: "a", items: []string{"from_a"}})
	b.AddSource(fakeSource{name: "b", items: []string{"from_b"}})
	b.PatchGlobal(cmp.Config{Sources: []string{"a"}})
	return b
}

func TestRuntimePatchLayers(t *testing.T) {
	// Order, widest to narrowest: global, filetype, buffer; at each scope the
	// Go setting comes before the one made in Vim script.
	b := twoSources()
	b.PatchFiletype("go", cmp.Config{Sources: []string{"b"}})

	for name, tc := range map[string]struct {
		ft      string
		patches map[string]any
		want    []string
	}{
		"nothing":     {"", nil, []string{"from_a"}},
		"vim global":  {"", map[string]any{"global": map[string]any{"sources": []any{"b"}}}, []string{"from_b"}},
		"go filetype": {"go", nil, []string{"from_b"}},
		"vim filetype beats go filetype": {"go", map[string]any{"filetype": map[string]any{
			"go": map[string]any{"sources": []any{"a"}}}}, []string{"from_a"}},
		"vim global does not beat go filetype": {"go", map[string]any{"global": map[string]any{
			"sources": []any{"a", "b"}}}, []string{"from_b"}},
		"buffer beats everything": {"go", map[string]any{
			"filetype": map[string]any{"go": map[string]any{"sources": []any{"a"}}},
			"buffer":   map[string]any{"sources": []any{"a", "b"}}}, []string{"from_a", "from_b"}},
		"filetype patch of another filetype is ignored": {"go", map[string]any{"filetype": map[string]any{
			"rust": map[string]any{"sources": []any{"a"}}}}, []string{"from_b"}},
	} {
		t.Run(name, func(t *testing.T) {
			h := uiHost()
			complete(t, b, h, withPatches(ctxMap(1, "f", tc.ft), tc.patches))
			if _, w := shown(t, h); !reflect.DeepEqual(w, tc.want) {
				t.Errorf("words = %q, want %q", w, tc.want)
			}
		})
	}
}

func TestRuntimePatchMapsMerge(t *testing.T) {
	// A buffer patch for one source option keeps the rest of the options.
	b := newBuilder()
	b.AddSource(fakeSource{name: "a", items: []string{"alpha", "beta"}})
	b.PatchGlobal(cmp.Config{
		Sources: []string{"a"},
		SourceOptions: map[string]cmp.SourceOption{
			"a": {Mark: "A", MaxItems: new(5)},
		},
	})
	h := uiHost()
	complete(t, b, h, withPatches(ctxMap(1, "x", ""), map[string]any{
		"buffer": map[string]any{"sourceOptions": map[string]any{"a": map[string]any{"maxItems": 1}}},
	}))
	var items []cmp.Item
	for _, c := range h.Calls() {
		if c.Fn == "goda_cmp_testui#insert#show" {
			items = c.Args[0].(map[string]any)["items"].([]cmp.Item)
		}
	}
	if len(items) != 1 || items[0].Menu != "A" {
		t.Errorf("items = %+v, want one item with the mark A kept", items)
	}
}

func TestRuntimePatchErrors(t *testing.T) {
	b := twoSources()
	for name, tc := range map[string]struct {
		patches map[string]any
		want    string
	}{
		"unknown key":    {map[string]any{"buffer": map[string]any{"sourcez": []any{"a"}}}, "sourcez"},
		"wrong type":     {map[string]any{"buffer": map[string]any{"sources": "a"}}, "sources"},
		"unknown source": {map[string]any{"buffer": map[string]any{"sources": []any{"nope"}}}, "unknown source"},
		"unknown scope":  {map[string]any{"buffr": map[string]any{}}, "buffr"},
	} {
		t.Run(name, func(t *testing.T) {
			err := completeErr(t, b, uiHost(), withPatches(ctxMap(1, "f", ""), tc.patches))
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Errorf("err = %v, want one mentioning %q", err, tc.want)
			}
		})
	}
}

func TestValidateHandler(t *testing.T) {
	p, _ := twoSources().Plugin()
	validate := p.Handlers()["validate"]
	if _, err := validate(context.Background(), nil, []any{map[string]any{"sources": []any{"a", "b"}, "ui": "every"}}); err != nil {
		t.Errorf("valid config: %v", err)
	}
	for name, cfg := range map[string]map[string]any{
		"typo":           {"sourse": []any{"a"}},
		"unknown source": {"sources": []any{"zzz"}},
		"unknown ui":     {"ui": "popup"},
		"unknown filter": {"sourceOptions": map[string]any{"a": map[string]any{"matchers": []any{"m"}}}},
		"bad regexp":     {"sourceOptions": map[string]any{"_": map[string]any{"keywordPattern": "[a-"}}},
		"bad force":      {"sourceOptions": map[string]any{"_": map[string]any{"forceCompletionPattern": "(x"}}},
	} {
		if _, err := validate(context.Background(), nil, []any{cfg}); err == nil {
			t.Errorf("%s: no error", name)
		}
	}
}

func TestOptionsHandlerSeesPatches(t *testing.T) {
	p, _ := twoSources().Plugin()
	got, err := p.Handlers()["options"](context.Background(), nil, []any{"go", map[string]any{
		"buffer": map[string]any{"ui": "every", "autoComplete": false, "autoCompleteDelay": 50},
	}})
	if err != nil {
		t.Fatal(err)
	}
	m := got.(map[string]any)
	if m["ui"] != "every" || m["autoComplete"] != false || m["delay"] != 50 {
		t.Errorf("options = %v", m)
	}
}

func TestKeywordPattern(t *testing.T) {
	// The default stops at '-', the pattern of a command line does not.
	b := newBuilder()
	b.AddSource(fakeSource{name: "a", items: []string{"foo-bar"}})
	b.PatchGlobal(cmp.Config{Sources: []string{"a"}})

	h := uiHost()
	complete(t, b, h, ctxMap(1, "x foo-b", ""))
	if col, _ := shown(t, h); col != 7 { // after "x foo-"
		t.Errorf("default: col = %d, want 7", col)
	}

	h = uiHost()
	complete(t, b, h, withPatches(ctxMap(2, "x foo-b", ""), map[string]any{
		"buffer": map[string]any{"sourceOptions": map[string]any{
			"_": map[string]any{"keywordPattern": `[0-9a-zA-Z_:#*/.-]*`}}},
	}))
	if col, _ := shown(t, h); col != 3 { // after "x "
		t.Errorf("with keywordPattern: col = %d, want 3", col)
	}

	// A pattern that matches nothing before the cursor completes at the cursor.
	h = uiHost()
	complete(t, b, h, withPatches(ctxMap(3, "ab ", ""), map[string]any{
		"buffer": map[string]any{"sourceOptions": map[string]any{
			"_": map[string]any{"keywordPattern": `\w+`, "minAutoCompleteLength": 0}}},
	}))
	if col, _ := shown(t, h); col != 4 {
		t.Errorf("no match: col = %d, want 4", col)
	}
}

func TestNamedSourceAndFilter(t *testing.T) {
	b := newBuilder()
	b.AddSource(fakeSource{name: "a", items: []string{"one"}})
	b.AddSource(cmp.NamedSource("a2", fakeSource{name: "a", items: []string{"two"}}))
	b.PatchGlobal(cmp.Config{Sources: []string{"a", "a2"}})
	h := uiHost()
	complete(t, b, h, ctxMap(1, "o", ""))
	if _, w := shown(t, h); !reflect.DeepEqual(w, []string{"one", "two"}) {
		t.Errorf("words = %q", w)
	}
	if got := cmp.NamedFilter("x", nopFilter{}).Name(); got != "x" {
		t.Errorf("name = %q", got)
	}
}

type nopFilter struct{}

func (nopFilter) Name() string { return "nop" }
func (nopFilter) Filter(_ context.Context, in cmp.FilterInput) ([]cmp.Item, error) {
	return in.Items, nil
}

func TestDynamicHooks(t *testing.T) {
	// The hooks are Go functions that may ask the editor, like dynamicSources
	// and dynamicUi in ddc.ts.
	run := func(mode string) (*fakeUI, *cmptest.Host) {
		ui := &fakeUI{name: "inline"}
		b := twoSources()
		b.AddUI(ui)
		b.PatchGlobal(cmp.Config{
			DynamicSources: func(ctx context.Context, h sdk.Host, a cmp.DynamicSourcesArgs) ([]string, error) {
				m, err := h.Call(ctx, "mode")
				if err != nil {
					return nil, err
				}
				if m == "c" {
					return append([]string{"b"}, a.Sources...), nil
				}
				return nil, nil
			},
			DynamicUI: func(_ context.Context, _ sdk.Host, a cmp.DynamicUIArgs) (string, error) {
				if len(a.Items) == 1 {
					return "inline", nil
				}
				return "", nil
			},
		})
		h := uiHost()
		h.Funcs["mode"] = func(...any) (any, error) { return mode, nil }
		complete(t, b, h, ctxMap(1, "f", ""))
		return ui, h
	}

	// One source, one item: the dynamic UI takes over.
	if ui, _ := run("i"); !reflect.DeepEqual(ui.shown, []string{"from_a"}) {
		t.Errorf("insert mode: inline UI got %q", ui.shown)
	}
	// The hook adds a source, so there are two items: the configured UI.
	ui, h := run("c")
	if len(ui.shown) != 0 {
		t.Errorf("two items: the inline UI should not be used, got %q", ui.shown)
	}
	if _, w := shown(t, h); !reflect.DeepEqual(w, []string{"from_b", "from_a"}) {
		t.Errorf("words = %q", w)
	}
}

// fakeUI records the words it was asked to show.
type fakeUI struct {
	name  string
	shown []string
}

func (u *fakeUI) Name() string { return u.name }
func (u *fakeUI) Show(_ context.Context, _ sdk.Host, r cmp.ShowRequest) error {
	for _, it := range r.Items {
		u.shown = append(u.shown, it.Word)
	}
	return nil
}
func (u *fakeUI) Hide(context.Context, sdk.Host) error { return nil }

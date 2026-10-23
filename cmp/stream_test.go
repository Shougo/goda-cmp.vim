package cmp_test

import (
	"context"
	"errors"
	"reflect"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Shougo/goda-cmp.vim/cmp"
	"github.com/Shougo/goda-cmp.vim/cmp/cmptest"
	"github.com/Shougo/goda.vim/sdk"
)

// streamSource emits each step (after its delay) and then returns err.
type streamSource struct {
	name  string
	steps []step
	err   error
}

type step struct {
	delay time.Duration
	words []string
}

func (s streamSource) Name() string { return s.name }
func (s streamSource) Gather(ctx context.Context, h sdk.Host, c *cmp.Context) (cmp.Gathered, error) {
	return cmp.Collect(ctx, h, c, s)
}
func (s streamSource) GatherStream(ctx context.Context, _ sdk.Host, c *cmp.Context, emit func(cmp.Gathered) error) error {
	for _, st := range s.steps {
		select {
		case <-time.After(st.delay):
		case <-ctx.Done():
			return ctx.Err()
		}
		g := cmp.Gathered{Start: c.KeywordStart()}
		for _, w := range st.words {
			g.Items = append(g.Items, cmp.Item{Word: w})
		}
		if err := emit(g); err != nil {
			return err
		}
	}
	return s.err
}

type show struct {
	words []string
	final bool
}

func shows(h *cmptest.Host) []show {
	var out []show
	for _, c := range h.Calls() {
		switch c.Fn {
		case "goda_cmp#ui#native#show":
			m := c.Args[0].(map[string]any)
			var s show
			for _, it := range m["items"].([]cmp.Item) {
				s.words = append(s.words, it.Word)
			}
			s.final = m["final"].(bool)
			out = append(out, s)
		case "goda_cmp#ui#native#hide":
			out = append(out, show{final: true})
		}
	}
	return out
}

func TestStreamPartialResults(t *testing.T) {
	b := newBuilder()
	b.AddSource(streamSource{name: "lsp", steps: []step{
		{0, []string{"fa"}},
		{150 * time.Millisecond, []string{"fa", "fb"}}, // replaces, not appends
	}})
	b.AddSource(fakeSource{name: "plain", items: []string{"fc"}})
	b.PatchGlobal(cmp.Config{Sources: []string{"lsp", "plain"}, PartialInterval: new(10)})

	h := uiHost()
	complete(t, b, h, ctxMap(1, "f", ""))
	got := shows(h)
	if len(got) < 2 {
		t.Fatalf("want at least one partial and one final update, got %+v", got)
	}
	if got[0].final {
		t.Errorf("first update must be partial: %+v", got)
	}
	last := got[len(got)-1]
	if !last.final || !reflect.DeepEqual(last.words, []string{"fa", "fb", "fc"}) {
		t.Errorf("last update = %+v, want final [fa fb fc]", last)
	}
	for _, s := range got[:len(got)-1] {
		if s.final {
			t.Errorf("only the last update is final: %+v", got)
		}
	}
}

func TestStreamKeepsEmittedItemsOnError(t *testing.T) {
	b := newBuilder()
	b.AddSource(streamSource{name: "lsp", steps: []step{{0, []string{"fa"}}}, err: errors.New("server died")})
	b.PatchGlobal(cmp.Config{Sources: []string{"lsp"}})
	h := uiHost()
	complete(t, b, h, ctxMap(1, "f", ""))
	got := shows(h)
	if last := got[len(got)-1]; !last.final || !reflect.DeepEqual(last.words, []string{"fa"}) {
		t.Errorf("got %+v", got)
	}
}

func TestStreamKeepsEmittedItemsOnTimeout(t *testing.T) {
	b := newBuilder()
	b.AddSource(streamSource{name: "lsp", steps: []step{{0, []string{"fa"}}, {5 * time.Second, []string{"fa", "fz"}}}})
	b.PatchGlobal(cmp.Config{Sources: []string{"lsp"}, SourceTimeout: new(100)})
	h := uiHost()
	start := time.Now()
	complete(t, b, h, ctxMap(1, "f", ""))
	if time.Since(start) > 2*time.Second {
		t.Errorf("did not stop at the timeout")
	}
	got := shows(h)
	if last := got[len(got)-1]; !last.final || !reflect.DeepEqual(last.words, []string{"fa"}) {
		t.Errorf("got %+v", got)
	}
}

func TestPlainSourcesShowOnce(t *testing.T) {
	b := twoSources()
	b.PatchGlobal(cmp.Config{Sources: []string{"a", "b"}})
	h := uiHost()
	complete(t, b, h, ctxMap(1, "f", ""))
	if got := shows(h); len(got) != 1 || !got[0].final {
		t.Errorf("want one final update, got %+v", got)
	}
}

func TestStreamSupersededRequestStops(t *testing.T) {
	b := newBuilder()
	var stopped atomic.Bool
	b.AddSource(stopSource{streamSource{name: "lsp", steps: []step{{0, []string{"fa"}}, {5 * time.Second, []string{"fb"}}}}, &stopped})
	b.PatchGlobal(cmp.Config{Sources: []string{"lsp"}, PartialInterval: new(10)})
	p, err := b.Plugin()
	if err != nil {
		t.Fatal(err)
	}
	h := uiHost()
	var wg sync.WaitGroup
	wg.Go(func() {
		p.Handlers()["complete"](context.Background(), h, []any{ctxMap(1, "f", "")})
	})
	time.Sleep(200 * time.Millisecond)
	p.Handlers()["complete"](context.Background(), h, []any{ctxMap(2, "f", "")}) // takes seq 2 and ends at its timeout... cancel via hide
	wg.Wait()
	if !stopped.Load() {
		t.Errorf("the superseded stream was not cancelled")
	}
}

type stopSource struct {
	streamSource
	stopped *atomic.Bool
}

func (s stopSource) GatherStream(ctx context.Context, h sdk.Host, c *cmp.Context, emit func(cmp.Gathered) error) error {
	err := s.streamSource.GatherStream(ctx, h, c, emit)
	if c.Seq == 1 && err != nil {
		s.stopped.Store(true)
	}
	return err
}

// ---------------------------------------------------------------------------

type lspSource struct {
	name     string
	inits    atomic.Int32
	initErr  error
	mu       sync.Mutex
	done     []cmp.DoneArgs
	resolved []cmp.DoneArgs
}

func (s *lspSource) Name() string { return s.name }
func (s *lspSource) Init(context.Context, sdk.Host) error {
	s.inits.Add(1)
	time.Sleep(50 * time.Millisecond)
	return s.initErr
}
func (s *lspSource) Gather(_ context.Context, _ sdk.Host, c *cmp.Context) (cmp.Gathered, error) {
	return cmp.Gathered{Start: c.KeywordStart(), Items: []cmp.Item{
		{Word: "print", FilterText: "printf", SortText: "2", UserData: map[string]any{"id": 7}},
		{Word: "other", SortText: "1"},
	}}, nil
}
func (s *lspSource) OnCompleteDone(_ context.Context, _ sdk.Host, a cmp.DoneArgs) error {
	s.mu.Lock()
	s.done = append(s.done, a)
	s.mu.Unlock()
	return nil
}
func (s *lspSource) Resolve(_ context.Context, _ sdk.Host, a cmp.DoneArgs) (cmp.Item, error) {
	s.mu.Lock()
	s.resolved = append(s.resolved, a)
	s.mu.Unlock()
	it := a.Item
	it.Info = "docs for " + it.Word
	return it, nil
}

func refsOf(h *cmptest.Host) map[string]string {
	refs := map[string]string{}
	for _, c := range h.Calls() {
		if c.Fn == "goda_cmp#ui#native#show" {
			for _, it := range c.Args[0].(map[string]any)["items"].([]cmp.Item) {
				refs[it.Word] = it.Ref
			}
		}
	}
	return refs
}

func TestInitDoneResolve(t *testing.T) {
	src := &lspSource{name: "lsp"}
	b := newBuilder()
	b.AddSource(src)
	b.PatchGlobal(cmp.Config{Sources: []string{"lsp"}})
	p, err := b.Plugin()
	if err != nil {
		t.Fatal(err)
	}
	h := uiHost()
	var wg sync.WaitGroup
	for i := range 3 { // Init runs once even when requests overlap
		wg.Go(func() {
			p.Handlers()["complete"](context.Background(), h, []any{ctxMap(int64(10+i), "p", "go")})
		})
	}
	wg.Wait()
	p.Handlers()["complete"](context.Background(), h, []any{ctxMap(20, "p", "go")})
	if n := src.inits.Load(); n != 1 {
		t.Errorf("Init ran %d times, want 1", n)
	}

	refs := refsOf(h)
	ref := refs["print"]
	if ref == "" {
		t.Fatalf("items carry no user_data reference: %v", refs)
	}

	out, err := p.Handlers()["resolve"](context.Background(), h, []any{ref})
	if err != nil {
		t.Fatal(err)
	}
	if m, _ := out.(map[string]any); m["info"] != "docs for print" {
		t.Errorf("resolve = %v", out)
	}
	if _, err := p.Handlers()["done"](context.Background(), h, []any{ref}); err != nil {
		t.Fatal(err)
	}
	if len(src.done) != 1 {
		t.Fatalf("OnCompleteDone calls = %d", len(src.done))
	}
	got := src.done[0]
	if got.Item.Word != "print" || got.Item.Info != "docs for print" ||
		!reflect.DeepEqual(got.Item.UserData, map[string]any{"id": 7}) || got.Context.Filetype != "go" {
		t.Errorf("OnCompleteDone got %+v", got)
	}

	if _, err := p.Handlers()["done"](context.Background(), h, []any{"999:0"}); err == nil {
		t.Errorf("an unknown reference must be an error")
	}
}

func TestInitFailureDisablesSource(t *testing.T) {
	src := &lspSource{name: "lsp", initErr: errors.New("no server")}
	b := newBuilder()
	b.AddSource(src)
	b.AddSource(fakeSource{name: "a", items: []string{"alpha"}})
	b.PatchGlobal(cmp.Config{Sources: []string{"lsp", "a"}})
	h := uiHost()
	complete(t, b, h, ctxMap(1, "a", ""))
	if _, w := shown(t, h); !reflect.DeepEqual(w, []string{"alpha"}) {
		t.Errorf("words = %q", w)
	}
}

func TestFilterTextAndSortText(t *testing.T) {
	it := cmp.Item{Word: "print", FilterText: "printf", SortText: "2"}
	if it.Filter() != "printf" || it.Sort() != "2" {
		t.Errorf("%q %q", it.Filter(), it.Sort())
	}
	if w := (cmp.Item{Word: "x"}); w.Filter() != "x" || w.Sort() != "x" {
		t.Errorf("fallbacks are wrong")
	}
}

type closeSource struct {
	fakeSource
	log *[]string
	err error
}

func (s closeSource) Close(context.Context) error {
	*s.log = append(*s.log, s.name)
	return s.err
}

func TestEngineClose(t *testing.T) {
	var log []string
	b := newBuilder()
	b.AddSource(closeSource{fakeSource{name: "a"}, &log, nil})
	b.AddSource(fakeSource{name: "plain"})
	b.AddSource(closeSource{fakeSource{name: "b"}, &log, errors.New("boom")})
	b.PatchGlobal(cmp.Config{Sources: []string{"a"}})
	p, err := b.Plugin()
	if err != nil {
		t.Fatal(err)
	}
	c, ok := p.(interface{ Close(context.Context) error }) // sdk.Closer
	if !ok {
		t.Fatal("the plugin must implement Close like sdk.Closer")
	}
	err = c.Close(context.Background())
	if !reflect.DeepEqual(log, []string{"b", "a"}) {
		t.Errorf("close order = %v, want [b a]", log)
	}
	if err == nil {
		t.Errorf("the error of a source's Close is lost")
	}
}

type previewSource struct{ fakeSource }

func (s previewSource) Preview(_ context.Context, _ sdk.Host, a cmp.DoneArgs) (cmp.Preview, error) {
	switch a.Item.Word {
	case "alpha":
		return cmp.Preview{Kind: cmp.PreviewHelp, Tag: a.Item.Word + "()"}, nil
	case "alps":
		return cmp.Preview{}, nil // no kind: empty
	}
	return cmp.Preview{Kind: cmp.PreviewText, Contents: []string{"one", "two"}}, nil
}

// hostFilter records the Host it was given. The sources of a request are
// filtered concurrently.
type hostFilter struct {
	mu  *sync.Mutex
	got *[]sdk.Host
}

func (hostFilter) Name() string { return "hostfilter" }
func (f hostFilter) Filter(_ context.Context, in cmp.FilterInput) ([]cmp.Item, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	*f.got = append(*f.got, in.Host)
	return in.Items, nil
}

func TestPreview(t *testing.T) {
	var hosts []sdk.Host
	b := newBuilder()
	b.AddSource(previewSource{fakeSource{name: "p", items: []string{"alpha", "alps", "amber"}}})
	b.AddSource(fakeSource{name: "plain", items: []string{"apple"}})
	b.AddFilter(hostFilter{&sync.Mutex{}, &hosts})
	b.PatchGlobal(cmp.Config{
		Sources:       []string{"p", "plain"},
		SourceOptions: map[string]cmp.SourceOption{"_": {Sorters: []string{"hostfilter"}}},
	})
	p, err := b.Plugin()
	if err != nil {
		t.Fatal(err)
	}
	h := uiHost()
	p.Handlers()["complete"](context.Background(), h, []any{ctxMap(1, "a", "")})
	if len(hosts) == 0 || hosts[0] != sdk.Host(h) {
		t.Errorf("filters got hosts %v", hosts)
	}
	refs := refsOf(h)
	for word, want := range map[string]any{
		"alpha": map[string]any{"kind": "help", "tag": "alpha()"},
		"alps":  map[string]any{"kind": "empty"},
		"amber": map[string]any{"kind": "text", "contents": []any{"one", "two"}},
		"apple": map[string]any{"kind": "empty"}, // not a Previewer
	} {
		got, err := p.Handlers()["preview"](context.Background(), h, []any{refs[word]})
		if err != nil || !reflect.DeepEqual(got, want) {
			t.Errorf("preview %s = %#v, %v; want %#v", word, got, err, want)
		}
	}
	if _, err := p.Handlers()["preview"](context.Background(), h, []any{"999:0"}); err == nil {
		t.Errorf("an unknown reference must be an error")
	}
}

package cmp_test

import (
	"context"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Shougo/goda-cmp.vim/cmp"
	"github.com/Shougo/goda-cmp.vim/cmp/cmptest"
	"github.com/Shougo/goda.vim/sdk"
)

type eventSource struct {
	fakeSource
	specs []cmp.EventSpec

	mu  sync.Mutex
	got []string
}

func (s *eventSource) Events() []cmp.EventSpec { return s.specs }
func (s *eventSource) OnEvent(_ context.Context, _ sdk.Host, ev cmp.Event) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.got = append(s.got, ev.Name+":"+ev.Path)
	return nil
}

func (s *eventSource) events() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.got...)
}

func waitEvents(t *testing.T, s *eventSource, n int) []string {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for len(s.events()) < n && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	return s.events()
}

func TestEvents(t *testing.T) {
	gosrc := &eventSource{fakeSource: fakeSource{name: "go"}, specs: []cmp.EventSpec{
		{Events: []string{"BufEnter", "BufDelete"}, Filetypes: []string{"go"}},
	}}
	anysrc := &eventSource{fakeSource: fakeSource{name: "any"}, specs: []cmp.EventSpec{
		{Events: []string{"BufDelete"}},
	}}
	b := newBuilder()
	b.AddSource(gosrc).AddSource(anysrc).AddSource(fakeSource{name: "plain"})
	p, err := b.Plugin()
	if err != nil {
		t.Fatal(err)
	}
	hs := p.Handlers()
	ctx := context.Background()

	// What the Vim side listens to.
	table, err := hs["events"](ctx, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	want := []any{
		map[string]any{"event": "BufDelete", "filetypes": []string{}}, // "any" wants every filetype
		map[string]any{"event": "BufEnter", "filetypes": []string{"go"}},
	}
	if !reflect.DeepEqual(table, want) {
		t.Errorf("events = %v, want %v", table, want)
	}

	h := &cmptest.Host{}
	send := func(seq int64, name, ft, path string) {
		t.Helper()
		if _, err := hs["event"](ctx, h, []any{map[string]any{
			"seq": seq, "name": name, "bufnr": int64(1), "tick": int64(1), "filetype": ft, "buftype": "", "path": path,
		}}); err != nil {
			t.Fatal(err)
		}
	}
	// Seq 2 arrives before 1: delivered in Vim's order. A dotted filetype
	// matches by its parts; other filetypes do not reach "go".
	send(2, "BufDelete", "go.tmpl", "/a.go")
	send(1, "BufEnter", "go.tmpl", "/a.go")
	send(3, "BufEnter", "text", "/b.txt")
	send(4, "BufDelete", "text", "/b.txt")
	if got := waitEvents(t, gosrc, 2); !reflect.DeepEqual(got, []string{"BufEnter:/a.go", "BufDelete:/a.go"}) {
		t.Errorf("go got %q", got)
	}
	if got := waitEvents(t, anysrc, 2); !reflect.DeepEqual(got, []string{"BufDelete:/a.go", "BufDelete:/b.txt"}) {
		t.Errorf("any got %q", got)
	}

	// A missing event is waited for briefly, then skipped.
	send(6, "BufDelete", "go", "/c.go")
	if got := waitEvents(t, gosrc, 3); len(got) != 3 || got[2] != "BufDelete:/c.go" {
		t.Errorf("after a gap: %q", got)
	}

	if err := p.(interface{ Close(context.Context) error }).Close(ctx); err != nil {
		t.Fatal(err)
	}
	send(7, "BufDelete", "go", "/d.go") // after Close: ignored, no panic
}

func TestBadEventName(t *testing.T) {
	src := &eventSource{fakeSource: fakeSource{name: "s"}, specs: []cmp.EventSpec{{Events: []string{"BufEnter | !rm"}}}}
	_, err := newBuilder().AddSource(src).Plugin()
	if err == nil || !strings.Contains(err.Error(), "bad event name") {
		t.Errorf("err = %v", err)
	}
}

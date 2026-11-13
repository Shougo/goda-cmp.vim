package cmp_test

import (
	"context"
	"fmt"
	"sync"
	"testing"

	"github.com/Shougo/goda-cmp.vim/cmp"
	"github.com/Shougo/goda-cmp.vim/cmp/cmptest"
	"github.com/Shougo/goda.vim/sdk"
)

func TestSupersededShowsNothing(t *testing.T) {
	// A request superseded while its dynamicUi runs shows nothing, hides
	// nothing, and leaves the UI shown alone: the inline UI the newer
	// request shows is hidden when the pum UI shows next.
	entered := make(chan struct{})
	release := make(chan struct{})
	b := cmp.New().
		AddUI(cmptest.NewUI("pum")).
		AddUI(cmptest.NewUI("inline")).
		AddSource(fakeSource{name: "a", items: []string{"from_a"}}).
		PatchGlobal(cmp.Config{
			UI:      "pum",
			Sources: []string{"a"},
			DynamicUI: func(_ context.Context, _ sdk.Host, a cmp.DynamicUIArgs) (string, error) {
				switch a.Context.Seq {
				case 2:
					// Slow (the editor is asked): superseded meanwhile.
					close(entered)
					<-release
					return "inline", nil
				case 3:
					return "inline", nil
				}
				return "", nil
			},
		})
	p, err := b.Plugin()
	if err != nil {
		t.Fatal(err)
	}
	h := cmptest.Lines()
	var mu sync.Mutex
	var log []string
	for _, ui := range []string{"pum", "inline"} {
		for _, fn := range []string{"show", "hide"} {
			name := ui + "#" + fn
			h.Funcs["goda_cmp_testui#"+name] = func(args ...any) (any, error) {
				l := name
				if fn == "show" {
					l += fmt.Sprintf("@%d", args[0].(map[string]any)["seq"])
				}
				mu.Lock()
				log = append(log, l)
				mu.Unlock()
				return nil, nil
			}
		}
	}
	complete := func(seq int64) {
		if _, err := p.Handlers()["complete"](context.Background(), h, []any{ctxMap(seq, "f", "")}); err != nil {
			t.Errorf("complete %d: %v", seq, err)
		}
	}

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		complete(2)
	}()
	<-entered
	complete(3)
	close(release)
	wg.Wait()
	cmptest.Wait(p)
	complete(4)
	cmptest.Wait(p)

	mu.Lock()
	defer mu.Unlock()
	for _, l := range log {
		if l == "pum#show@2" || l == "inline#show@2" {
			t.Errorf("the superseded request showed: %q", log)
		}
	}
	// The last calls: the inline UI hidden, the pum UI shown.
	n := len(log)
	if n < 2 || log[n-2] != "inline#hide" || log[n-1] != "pum#show@4" {
		t.Errorf("calls = %q, want inline#hide, pum#show@4 at the end", log)
	}
}

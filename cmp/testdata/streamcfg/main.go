// A configuration for the real-Vim test of streaming and OnCompleteDone: one
// source that behaves like a language server (a fast answer, a complete one
// later) and records what it is told about accepted items.
package main

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/Shougo/goda-cmp.vim/cmp"
	"github.com/Shougo/goda-cmp.vim/ext/filter/matcherhead"
	"github.com/Shougo/goda.vim/sdk"
)

type fakeLSP struct{}

func (fakeLSP) Name() string { return "fakelsp" }
func (s fakeLSP) Gather(ctx context.Context, h sdk.Host, c *cmp.Context) (cmp.Gathered, error) {
	return cmp.Collect(ctx, h, c, s)
}
func (fakeLSP) GatherStream(ctx context.Context, _ sdk.Host, c *cmp.Context, emit func(cmp.Gathered) error) error {
	g := cmp.Gathered{Start: c.KeywordStart(), Items: []cmp.Item{{Word: "alpha_fast", UserData: "id-1"}}}
	if err := emit(g); err != nil {
		return err
	}
	select {
	case <-time.After(800 * time.Millisecond):
	case <-ctx.Done():
		return ctx.Err()
	}
	g.Items = append(g.Items, cmp.Item{Word: "alpha_late", UserData: "id-2"})
	return emit(g)
}
func (fakeLSP) OnCompleteDone(_ context.Context, _ sdk.Host, a cmp.DoneArgs) error {
	f, err := os.OpenFile(os.Getenv("GODA_DONE_FILE"), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = fmt.Fprintf(f, "%s %v\n", a.Item.Word, a.Item.UserData)
	return err
}

func main() {
	b := cmp.New()
	b.AddSource(fakeLSP{})
	b.AddFilter(matcherhead.New(matcherhead.Params{}))
	b.PatchGlobal(cmp.Config{
		UI:                "native",
		Sources:           []string{"fakelsp"},
		SourceOptions:     map[string]cmp.SourceOption{"_": {Matchers: []string{"matcher_head"}}},
		AutoCompleteDelay: cmp.Ptr(10),
		PartialInterval:   cmp.Ptr(20),
	})
	b.Main()
}

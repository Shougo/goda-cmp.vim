// A configuration for the real-editor test of the Vim functions: around and
// matcherhead with the native popup; the rest is set from Vim script.
package main

import (
	"github.com/Shougo/goda-cmp.vim/cmp"
	"github.com/Shougo/goda-cmp.vim/cmp/cmptest"
	"github.com/Shougo/goda-cmp.vim/ext/filter/matcherhead"
	"github.com/Shougo/goda-cmp.vim/ext/source/around"
)

func main() {
	b := cmp.New()
	b.AddUI(cmptest.NewUI("test", "i", "c", "t"))
	b.AddSource(around.New(around.Params{}))
	b.AddFilter(matcherhead.New(matcherhead.Params{}))
	b.PatchGlobal(cmp.Config{
		UI:                "test",
		Sources:           []string{"around"},
		SourceOptions:     map[string]cmp.SourceOption{"_": {Matchers: []string{"matcherhead"}}},
		AutoCompleteDelay: new(10),
	})
	b.Main()
}

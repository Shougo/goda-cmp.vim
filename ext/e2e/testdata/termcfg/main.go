// A configuration for the real-editor test of terminal completion: around
// and matcherhead with pum.vim, in special buffers too.
package main

import (
	"github.com/Shougo/goda-cmp.vim/cmp"
	"github.com/Shougo/goda-cmp.vim/ext/filter/matcherhead"
	"github.com/Shougo/goda-cmp.vim/ext/source/around"
	"github.com/Shougo/goda-cmp.vim/ext/ui/pum"
)

func main() {
	b := cmp.New()
	b.AddUI(pum.New())
	b.AddSource(around.New(around.Params{}))
	b.AddFilter(matcherhead.New(matcherhead.Params{}))
	b.PatchGlobal(cmp.Config{
		UI:                      "pum",
		Sources:                 []string{"around"},
		SourceOptions:           map[string]cmp.SourceOption{"_": {Matchers: []string{"matcherhead"}}},
		SpecialBufferCompletion: new(true),
		AutoCompleteDelay:       new(10),
	})
	b.Main()
}

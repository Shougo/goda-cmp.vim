// A configuration for the real-editor test of terminal completion: around
// and matcherhead with pum.vim, in special buffers too.
package main

import (
	"github.com/Shougo/goda-cmp-ext/ext/filter/matcherhead"
	"github.com/Shougo/goda-cmp-ext/ext/source/around"
	"github.com/Shougo/goda-cmp-ui-pum.vim/ext/ui/pum"
	"github.com/Shougo/goda-cmp.vim/cmp"
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

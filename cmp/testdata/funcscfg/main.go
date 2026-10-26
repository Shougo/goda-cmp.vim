// A configuration for the real-editor test of the Vim functions: the test
// source and matcher with the test UI; the rest is set from Vim script.
package main

import (
	"github.com/Shougo/goda-cmp.vim/cmp"
	"github.com/Shougo/goda-cmp.vim/cmp/cmptest"
)

func main() {
	b := cmp.New()
	b.AddUI(cmptest.NewUI("test", "i", "c", "t"))
	b.AddSource(cmptest.NewWords(cmptest.WordsParams{}))
	b.AddFilter(cmptest.NewHead())
	b.PatchGlobal(cmp.Config{
		UI:                "test",
		Sources:           []string{"words"},
		SourceOptions:     map[string]cmp.SourceOption{"_": {Matchers: []string{"head"}}},
		AutoCompleteDelay: new(10),
	})
	b.Main()
}

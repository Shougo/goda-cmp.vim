// Command corecfg is the configuration of the core's end-to-end tests: the
// test UI, source and matcher of cmptest, so that no extension is involved.
// $GODA_WORDS are offered after the words of the buffer (and alone on the
// command line and in a terminal).
package main

import (
	"os"
	"strings"

	"github.com/Shougo/goda-cmp.vim/cmp"
	"github.com/Shougo/goda-cmp.vim/cmp/cmptest"
)

func main() {
	b := cmp.New()
	b.AddUI(cmptest.NewUI("test", "i", "c", "t"))
	b.AddSource(cmptest.NewWords(cmptest.WordsParams{Extra: strings.Fields(os.Getenv("GODA_WORDS"))}))
	b.AddFilter(cmptest.NewHead())
	b.PatchGlobal(cmp.Config{
		UI:                      "test",
		Sources:                 []string{"words"},
		CmdlineSources:          map[string][]string{":": {"words"}},
		SourceOptions:           map[string]cmp.SourceOption{"_": {Matchers: []string{"head"}}},
		SpecialBufferCompletion: new(true),
		AutoCompleteDelay:       new(10),
	})
	b.Main()
}

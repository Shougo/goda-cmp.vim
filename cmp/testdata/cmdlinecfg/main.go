// A configuration for the real-editor test of command-line completion: the
// cmdline and cmdlinehistory sources on ":", shown by the pum UI, with the
// history sorter.
package main

import (
	"github.com/Shougo/goda-cmp.vim/cmp"
	"github.com/Shougo/goda-cmp.vim/ext/filter/sortercmdlinehistory"
	"github.com/Shougo/goda-cmp.vim/ext/source/cmdline"
	"github.com/Shougo/goda-cmp.vim/ext/source/cmdlinehistory"
	"github.com/Shougo/goda-cmp.vim/ext/ui/pum"
)

func main() {
	b := cmp.New()
	b.AddUI(pum.New())
	b.AddSource(cmdline.New())
	b.AddSource(cmdlinehistory.New(cmdlinehistory.Params{}))
	b.AddFilter(sortercmdlinehistory.New(sortercmdlinehistory.Params{}))
	b.PatchGlobal(cmp.Config{
		UI:             "pum",
		CmdlineSources: map[string][]string{":": {"cmdline", "cmdlinehistory"}},
		SourceOptions: map[string]cmp.SourceOption{
			"cmdline":        {Mark: "cmdline", Sorters: []string{"sortercmdlinehistory"}},
			"cmdlinehistory": {Mark: "history"},
		},
		AutoCompleteDelay: new(10),
	})
	b.Main()
}

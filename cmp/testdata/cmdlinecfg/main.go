// A configuration for the real-editor test of command-line completion: the
// cmdline source on ":", shown by the pum UI.
package main

import (
	"github.com/Shougo/goda-cmp.vim/cmp"
	"github.com/Shougo/goda-cmp.vim/ext/source/cmdline"
	"github.com/Shougo/goda-cmp.vim/ext/ui/pum"
)

func main() {
	b := cmp.New()
	b.AddUI(pum.New())
	b.AddSource(cmdline.New())
	b.PatchGlobal(cmp.Config{
		UI:                "pum",
		CmdlineSources:    map[string][]string{":": {"cmdline"}},
		SourceOptions:     map[string]cmp.SourceOption{"cmdline": {Mark: "cmdline"}},
		AutoCompleteDelay: new(10),
	})
	b.Main()
}

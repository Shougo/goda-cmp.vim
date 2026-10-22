// An invalid configuration for the real-Vim test of configuration errors:
// a filter name that was never added (the name of an older version).
package main

import (
	"github.com/Shougo/goda-cmp.vim/cmp"
	"github.com/Shougo/goda-cmp.vim/ext/ui/native"
)

func main() {
	b := cmp.New()
	b.AddUI(native.New())
	b.PatchGlobal(cmp.Config{
		UI:            "native",
		SourceOptions: map[string]cmp.SourceOption{"_": {Matchers: []string{"matcher_head"}}},
	})
	b.Main()
}

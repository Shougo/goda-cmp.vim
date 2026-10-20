// An invalid configuration for the real-Vim test of configuration errors:
// a filter name that was never added (the name of an older version).
package main

import "github.com/Shougo/goda-cmp.vim/cmp"

func main() {
	b := cmp.New()
	b.PatchGlobal(cmp.Config{
		SourceOptions: map[string]cmp.SourceOption{"_": {Matchers: []string{"matcher_head"}}},
	})
	b.Main()
}

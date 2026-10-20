// The configuration of the real-Vim test of the lsp source: gopls as the
// only source, configured the way a user would.
package main

import (
	"os"

	"github.com/Shougo/goda-cmp.vim/cmp"
	"github.com/Shougo/goda-cmp.vim/ext/filter/sortertext"
	"github.com/Shougo/goda-cmp.vim/ext/source/lsp"
)

func main() {
	gopls := os.Getenv("GODA_GOPLS")
	if gopls == "" {
		gopls = "gopls"
	}
	b := cmp.New()
	b.AddSource(lsp.New(lsp.Params{
		Name:        "gopls",
		Command:     []string{gopls},
		Env:         []string{"GOPROXY=off", "GOTOOLCHAIN=local"},
		Filetypes:   []string{"go"},
		RootMarkers: []string{"go.work", "go.mod", ".git"},
	}))
	b.AddFilter(sortertext.New())
	b.PatchGlobal(cmp.Config{
		UI:      "native",
		Sources: []string{"gopls"},
		SourceOptions: map[string]cmp.SourceOption{
			// gopls filters by itself (fuzzy), so no matcher; its order is
			// in sortText. Right after "." there is no word yet: force.
			"gopls": {Mark: "LSP", ForceCompletionPattern: `\.\w*`, Sorters: []string{"sortertext"}},
		},
		AutoCompleteDelay: new(10),
		SourceTimeout:     new(5000),
	})
	b.Main()
}

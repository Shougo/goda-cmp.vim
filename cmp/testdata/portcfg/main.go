// A configuration for the real-editor test of the ported extensions, shaped
// like a ddc.vim user's: pum, or inline for a single candidate.
package main

import (
	"context"

	"github.com/Shougo/goda-cmp.vim/cmp"
	"github.com/Shougo/goda-cmp.vim/ext/filter/converterremoveoverlap"
	"github.com/Shougo/goda-cmp.vim/ext/filter/matcherhead"
	"github.com/Shougo/goda-cmp.vim/ext/filter/matcherlength"
	"github.com/Shougo/goda-cmp.vim/ext/filter/matcherprefix"
	"github.com/Shougo/goda-cmp.vim/ext/filter/sorterhead"
	"github.com/Shougo/goda-cmp.vim/ext/filter/sorterrank"
	"github.com/Shougo/goda-cmp.vim/ext/source/around"
	"github.com/Shougo/goda-cmp.vim/ext/source/buffer"
	"github.com/Shougo/goda-cmp.vim/ext/source/file"
	"github.com/Shougo/goda-cmp.vim/ext/source/input"
	"github.com/Shougo/goda-cmp.vim/ext/source/register"
	"github.com/Shougo/goda-cmp.vim/ext/source/shellnative"
	"github.com/Shougo/goda-cmp.vim/ext/source/vim"
	"github.com/Shougo/goda-cmp.vim/ext/ui/inline"
	"github.com/Shougo/goda-cmp.vim/ext/ui/pum"
	"github.com/Shougo/goda.vim/sdk"
)

func main() {
	b := cmp.New()
	b.AddUI(pum.New())
	b.AddUI(inline.New())
	b.AddSource(around.New(around.Params{}))
	b.AddSource(buffer.New(buffer.Params{RequireSameFiletype: new(false), ForceCollect: true}))
	b.AddSource(register.New(register.Params{ExtractWords: true}))
	b.AddSource(file.New(file.Params{FilenameChars: "[:keyword:]."}))
	b.AddSource(input.New())
	b.AddSource(vim.New())
	b.AddSource(shellnative.New(shellnative.Params{Shell: "zsh"}))
	for _, f := range []cmp.Filter{
		matcherhead.New(matcherhead.Params{}), matcherprefix.New(matcherprefix.Params{}),
		matcherlength.New(), sorterrank.New(), sorterhead.New(), converterremoveoverlap.New(),
	} {
		b.AddFilter(f)
	}
	b.PatchGlobal(cmp.Config{
		UI: "pum",
		DynamicUI: func(_ context.Context, _ sdk.Host, a cmp.DynamicUIArgs) (string, error) {
			if a.Context.Mode != "t" && len(a.Items) == 1 {
				return "inline", nil
			}
			return "pum", nil
		},
		Sources:        []string{"around", "buffer", "register", "file"},
		CmdlineSources: map[string][]string{"@": {"input"}, "=": {"input"}},
		SourceOptions: map[string]cmp.SourceOption{
			"_": {
				IgnoreCase: new(true),
				Matchers:   []string{"matcherhead", "matcherprefix", "matcherlength"},
				Sorters:    []string{"sorterrank"},
				Converters: []string{"converterremoveoverlap"},
			},
			"file":  {IsVolatile: new(true), VolatilePattern: "/", MinAutoCompleteLength: new(1000), ForceCompletionPattern: `\S/\S*`},
			"input": {IsVolatile: new(true), ForceCompletionPattern: `\S/\S*`},
			"vim":   {IsVolatile: new(true)},
			"shellnative": {
				ForceCompletionPattern: `\S/\S*`,
				MinAutoCompleteLength:  new(3),
				IsVolatile:             new(true),
				KeywordPattern:         `[0-9a-zA-Z_./#:-]*`,
			},
		},
		PostFilters:       []string{"sorterhead"},
		AutoCompleteDelay: new(10),
	})
	b.PatchFiletype("vim", cmp.Config{Sources: []string{"vim", "around"}})
	b.Main()
}

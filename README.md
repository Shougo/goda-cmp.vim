# goda-cmp.vim

Completion for Vim and Neovim, built on [goda.vim](https://github.com/Shougo/goda.vim).
A successor in spirit to ddc.vim: sources and filters are separate extensions,
the popup is either Vim's native one or [pum.vim](https://github.com/Shougo/pum.vim),
and the configuration is code — a small Go program you own.

Status: early. See "Not implemented yet".

## How it fits together

```
 ~/.config/nvim/goda-cmp/        your config module (Go)   <- you edit this
   go.mod  main.go  config.go
        │  imports
        ▼
 goda-cmp.vim/cmp                 engine, interfaces, native + pum UI
 goda-cmp-source-*, goda-cmp-filter-*   extensions (Go modules, own repositories)
        │
        ▼  go build  →  one binary, started by goda.vim (Vim or Neovim)
```

There is no plugin loader and no manifest: an extension is a Go package that
your `main.go` imports, so versions are pinned by your `go.mod`, and your
editor type-checks and completes the configuration.

## Install

You need Go (1.24 or newer). With a plugin manager, install `Shougo/goda.vim`,
`Shougo/goda-cmp.vim` and, if you use the pum UI, `Shougo/pum.vim`.

Create the config module (the default place is `stdpath('config')/goda-cmp` on
Neovim and `~/.vim/goda-cmp` on Vim; override with `g:goda_cmp_config_dir`):

```sh
mkdir -p ~/.config/nvim/goda-cmp && cd ~/.config/nvim/goda-cmp
cp /path/to/goda-cmp.vim/examples/config/{main.go,config.go} .
go mod init goda-cmp-config
go get github.com/Shougo/goda-cmp.vim@latest
```

Then in your vimrc / init.vim:

```vim
set completeopt=menuone,noinsert,noselect
autocmd VimEnter * call goda_cmp#enable()
inoremap <C-n> <Cmd>call goda_cmp#complete()<CR>
```

`goda_cmp#build()` runs `go build` in the config directory and writes the binary
next to it (`g:goda_cmp_binary` to put it elsewhere). Rebuild after editing
the config or updating an extension; then restart the editor (or
`goda_cmp#disable()` / `goda_cmp#enable()`).

## Configuration

`examples/config/` is a complete example. The shape, which mirrors `ddc.ts`:

```go
b := cmp.New()
b.AddSource(around.New(around.Params{MinLength: 2}))
b.AddFilter(matcherhead.New(matcherhead.Params{}))

b.PatchGlobal(cmp.Config{
    UI:      "pum", // or "native"
    Sources: []string{"around"},
    SourceOptions: map[string]cmp.SourceOption{
        "_":      {Matchers: []string{"matcher_head"}, Sorters: []string{"sorter_length"}},
        "around": {Mark: "A"},
    },
})
b.PatchFiletype("markdown", cmp.Config{ /* laid over the global settings */ })
b.Main()
```

Patches merge field by field: unset fields keep the previous value, maps are
merged by key, slices are replaced. Options named `"_"` are inherited by every
source. A name used in the configuration but never added (a typo) is reported
when the binary starts (`:GodaLog`).

Settings: `UI`, `Sources`, `SourceOptions` (`Mark`, `MinAutoCompleteLength`,
`MaxItems`, `Matchers`, `Sorters`, `Converters`), `PostFilters`,
`AutoComplete`, `AutoCompleteDelay`, `SourceTimeout`. Where zero is a real
value the field is a pointer: `cmp.Ptr(0)`.

### Popup

`UI: "native"` uses Vim's popup menu; `UI: "pum"` uses pum.vim, which you
configure in Vim script as usual (`pum#set_option`, `pum#map#...`). The UI can
differ per filetype.

## Writing an extension

An extension is a Go module that implements one of the interfaces in package
`cmp` and exports a constructor:

```go
type Source interface {
    Name() string
    Gather(ctx context.Context, h sdk.Host, c *cmp.Context) (cmp.Gathered, error)
}
type Filter interface {
    Name() string
    Filter(ctx context.Context, in cmp.FilterInput) ([]cmp.Item, error)
}
```

A source may call the editor through `h` (`h.Call(ctx, "getbufline", ...)`).
Whether a filter is a matcher, sorter or converter depends only on the list
it is placed in. A source that fails, panics or times out is skipped for that
request; the others still answer. `cmp/cmptest` offers a fake editor for unit
tests. See `ext/` for four small examples.

## Tests

```sh
go test ./...                      # unit tests; also starts a real Vim if present
GODA_CMP_PUM=/path/to/pum.vim go test ./cmp   # also run the pum scenario
```

The Vim test types into a real Vim in a pseudo terminal (Python 3 needed),
using a binary built from `examples/config`. goda.vim is expected as a sibling
directory (`../goda.vim`) while the modules are unpublished; the `replace`
lines in `go.mod` and `examples/config/go.mod` are to be removed after that.

## Not implemented yet

Compared with ddc.vim: command-line completion, `dynamicSources` /
`forceCompletionPattern`, volatile sources, per-source `dup`, `ignoreCase`
options, user-defined `isVolatile`, and snippet/`user_data` expansion hooks.
Text inserted by selecting an item is meant not to start a new request
(native: `complete_info()`, pum: `pum#skip_complete()`); this is untested.

## License

MIT, see LICENSE.

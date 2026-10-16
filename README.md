# goda-cmp.vim

Completion for Vim and Neovim, built on [goda.vim](https://github.com/Shougo/goda.vim).
A successor in spirit to ddc.vim: sources and filters are separate extensions,
the popup is either Vim's native one or [pum.vim](https://github.com/Shougo/pum.vim),
and the configuration is code — a small Go program you own.

Status: early. See "Not implemented yet".

## Try it

You need Go 1.24 or newer, and Vim 9.1 or newer (with `+job +channel +timers`)
or Neovim.

**1. Install the plugins** with your plugin manager: `Shougo/goda.vim` and
`Shougo/goda-cmp.vim`; add `Shougo/pum.vim` if you want the pum popup.

**2. Create your configuration**, a small Go module. The default place is
`~/.vim/goda-cmp` for Vim and `stdpath('config') . '/goda-cmp'`
(`~/.config/nvim/goda-cmp`) for Neovim; `g:goda_cmp_config_dir` overrides it.

```sh
mkdir -p ~/.vim/goda-cmp && cd ~/.vim/goda-cmp        # Neovim: ~/.config/nvim/goda-cmp
cp /path/to/goda-cmp.vim/examples/config/{main.go,config.go} .
go mod init goda-cmp-config
go get github.com/Shougo/goda-cmp.vim@latest
go mod tidy
```

(While the code is on a branch, replace `@latest` with `@wip` or the branch name.)

**3. Add this to your vimrc / init.vim:**

```vim
set completeopt=menuone,noinsert,noselect
autocmd VimEnter * call goda_cmp#enable()
inoremap <C-n> <Cmd>call goda_cmp#complete()<CR>   " complete now, whatever the length
```

**4. Build it and start:**

```vim
:call goda_cmp#build()      " runs `go build` in the config directory
:call goda_cmp#enable()
```

Until the first build, starting the editor prints a one-line hint instead.
Open a file, go into insert mode and type: words of the buffer are offered.
If nothing appears, `:echo goda#log()` shows what the plugin said, and
`:echo goda#plugins()` should list `cmp`.

The binary is written next to the configuration (`g:goda_cmp_binary` to put it
elsewhere) and has to be built again after you edit `main.go` or `config.go`,
or update goda.vim, goda-cmp.vim or an extension (`go get -u`, then rebuild).
`goda_cmp#rebuild()` stops, builds and starts again; for example

```vim
autocmd BufWritePost ~/.vim/goda-cmp/*.go call goda_cmp#rebuild()
```

If a build leaves you with a "protocol" error, the binary and the Vim script
come from different versions of goda.vim: rebuild.

To use pum.vim: `call goda_cmp#patch_global({'ui': 'pum'})` (or `UI: "pum"` in
`config.go`) and configure pum.vim as usual (`pum#set_option`, `pum#map#...`).

## How it fits together

```
 ~/.vim/goda-cmp/                 your config module (Go)   <- you edit this
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
when the binary starts (`echo goda#log()`).

Settings: `UI`, `Sources`, `SourceOptions` (`Mark`, `MinAutoCompleteLength`,
`MaxItems`, `KeywordPattern`, `Matchers`, `Sorters`, `Converters`),
`PostFilters`, `AutoComplete`, `AutoCompleteDelay`, `SourceTimeout`. Where zero
is a real value the field is a pointer: `cmp.Ptr(0)`.

`KeywordPattern` is a regular expression in Go's RE2 syntax (no lookaround); it
must match the text before the cursor that is being completed, ending at the
cursor. Set it under `"_"` for all sources or per source.

### Changing settings while Vim runs

The extensions are fixed when you build; the settings are not. As in ddc.vim,
Vim script can patch them at any time, for everything, for a filetype or for
the current buffer:

```vim
call goda_cmp#patch_global({'ui': 'pum'})
call goda_cmp#patch_filetype('go', {'sources': ['around']})

let l:prev = goda_cmp#get_buffer()
call goda_cmp#patch_buffer({'sourceOptions': {'_': {'keywordPattern': '[0-9a-zA-Z_:#*/.-]*'}}})
" ... later
call goda_cmp#set_buffer(l:prev)
```

A patch is a dictionary with the keys of `cmp.Config` (the names are the ones
ddc.vim uses). It is merged the same way as in Go. Settings are applied in this
order, the later one winning: global (Go, then Vim), filetype (Go, then Vim),
buffer. They are kept in Vim script, so they survive a restart of the binary,
go away with the buffer, and can be set before the binary is started. A patch
with an unknown key, a wrong type or a name that was never added is refused
with an error while the plugin is running.

Functions cannot come from Vim script. What needs logic goes in your Go
configuration, where it may ask the editor, like `dynamicSources` and
`dynamicUi` in ddc.ts:

```go
b.PatchGlobal(cmp.Config{
    DynamicSources: func(ctx context.Context, h sdk.Host, a cmp.DynamicSourcesArgs) ([]string, error) {
        mode, err := h.Call(ctx, "mode")
        if err != nil { return nil, err }
        if mode == "c" { return append([]string{"cmdline"}, a.Sources...), nil }
        return nil, nil // unchanged
    },
    DynamicUI: func(ctx context.Context, h sdk.Host, a cmp.DynamicUIArgs) (string, error) {
        if len(a.Items) == 1 { return "inline", nil } // a UI you added
        return "", nil // unchanged
    },
})
```

To use one source twice with different parameters, add it under another name:
`b.AddSource(cmp.NamedSource("around-md", around.New(around.Params{MinLength: 4})))`.

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

Compared with ddc.vim: command-line completion (`Context.Mode` is reserved for
it), `forceCompletionPattern`, volatile sources, `sourceParams` set from Vim
script (parameters are Go values given to the constructor), per-source `dup`, `ignoreCase`
options, user-defined `isVolatile`, and snippet/`user_data` expansion hooks.
Text inserted by selecting an item is meant not to start a new request
(native: `complete_info()`, pum: `pum#skip_complete()`); this is untested.

## License

MIT, see LICENSE.

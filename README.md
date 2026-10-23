# goda-cmp.vim

Completion for Vim and Neovim, built on [goda.vim](https://github.com/Shougo/goda.vim).
A successor in spirit to ddc.vim: sources and filters are separate extensions,
the popup is either Vim's native one or [pum.vim](https://github.com/Shougo/pum.vim),
and the configuration is code — a small Go program you own.

Status: early. See "Not implemented".

## Try it

You need Go 1.26 or newer, and Vim 9.1 or newer (with `+job +channel +timers`)
or Neovim. Install `Shougo/goda.vim` and `Shougo/goda-cmp.vim` (and
`Shougo/pum.vim` for the pum popup) with your plugin manager, then create your
configuration, a Go module of your own, outside this repository (keep it in
your dotfiles; `examples/config` is only a template to copy):

```sh
mkdir -p ~/.vim/goda-cmp && cd ~/.vim/goda-cmp        # Neovim: ~/.config/nvim/goda-cmp
cp /path/to/goda-cmp.vim/examples/config/{main.go,config.go} .
go mod init goda-cmp-config
go get github.com/Shougo/goda-cmp.vim@latest
go mod tidy
```

Add to your vimrc / init.vim:

```vim
autocmd VimEnter * call goda_cmp#enable()
```

Then build and start:

```vim
:call goda_cmp#build()
:call goda_cmp#enable()
```

Open a file, go into insert mode and type: words of the buffer are offered.
If nothing appears, `:echo goda#log()` shows what the plugin said.

Everything else is in `:help goda_cmp` ([doc/goda_cmp.txt](doc/goda_cmp.txt)):
where the files and the binary live, rebuilding and updating, the
configuration, run-time patches, selecting candidates, the popup, writing
extensions and partial results.

A configuration translated from a ddc.vim user's `ddc.ts`, with its Vim
script, is in [examples/ddc](examples/ddc).

## How it fits together

```
 ~/.vim/goda-cmp/                 your config module (Go)   <- you edit this
   go.mod  main.go  config.go
        │  imports
        ▼
 goda-cmp.vim/cmp                 engine and interfaces (no UI, no source)
 goda-cmp.vim/ext/...             bundled extensions: ui/native, ui/pum,
                                  ports of ddc.vim extensions:
                                  source/around, source/lsp, source/file, ...
                                  (`:help goda-cmp-bundled`)
 goda-cmp-source-*, goda-cmp-filter-*   extensions (Go modules, own repositories)
        │
        ▼  go build  →  one binary, started by goda.vim (Vim or Neovim)
```

There is no plugin loader and no manifest: an extension is a Go package that
your `main.go` imports, so versions are pinned by your `go.mod`, and your
editor type-checks and completes the configuration.

## Tests

```sh
go test ./...                      # unit tests; also real Vim and Neovim if present
GODA_CMP_PUM=/path/to/pum.vim go test ./cmp   # also run the pum scenario
```

The end-to-end tests type into a real Vim and a real Neovim, whichever are
installed, in a pseudo terminal (Python 3 needed), using a binary built from
`examples/config`; ext/source/lsp does the same with gopls if it is
installed. CI (`.github/workflows/ci.yml`) installs all of them. They build
against goda.vim as a sibling directory (`../goda.vim`), and are skipped
without it.

## Not implemented

Compared with ddc.vim: `ddc#callback()`, `ddc#register()` and
`ddc#custom#load_config()` (extensions are Go packages linked into the binary),
`matcherConcurrency` and the filter option `parallelSafe` (filters run as Go
code), `uiOptions` and `filterOptions` (ddc.vim defines none), and
`ddc#custom#set_context_*()` from Vim script (`Config.DynamicConfig` in Go
does it). Command-line and terminal completion are there
(`:help goda-cmp-cmdline`, `:help goda-cmp-terminal`); previews need a pum.vim
with its `previewer` option (`:help goda-cmp-preview`).

## License

MIT, see LICENSE.

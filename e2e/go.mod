module github.com/Shougo/goda-cmp.vim/e2e

go 1.26.0

require (
	github.com/Shougo/goda-cmp-ext v0.2.0
	github.com/Shougo/goda-cmp-ui-inline.vim v0.1.0
	github.com/Shougo/goda-cmp-ui-native.vim v0.1.0
	github.com/Shougo/goda-cmp-ui-pum.vim v0.1.0
	github.com/Shougo/goda-cmp.vim v0.8.1
)

require github.com/Shougo/goda.vim v0.12.0

// This goda-cmp.vim.
replace github.com/Shougo/goda-cmp.vim => ../

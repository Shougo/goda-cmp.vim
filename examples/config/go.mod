module goda-cmp-config

go 1.24

require (
	github.com/Shougo/goda-cmp.vim v0.0.0
	github.com/Shougo/goda.vim v0.0.0
)

// While the modules are not published; delete these two lines afterwards.
replace github.com/Shougo/goda-cmp.vim => ../..

replace github.com/Shougo/goda.vim => ../../../goda.vim

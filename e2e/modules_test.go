package e2e_test

// The modules of the configurations of testdata (which go mod tidy does not
// see): one package of each is imported, so that go.mod keeps them and
// BuildConfig has them.
import (
	_ "github.com/Shougo/goda-cmp-ext/ext/ui/none"
	_ "github.com/Shougo/goda-cmp-ui-inline.vim/ext/ui/inline"
	_ "github.com/Shougo/goda-cmp-ui-native.vim/ext/ui/native"
	_ "github.com/Shougo/goda-cmp-ui-pum.vim/ext/ui/pum"
)

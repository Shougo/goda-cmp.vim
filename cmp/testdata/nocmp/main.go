// A binary without the "cmp" plugin, for the real-Vim test of
// goda_cmp#enable() pointed at the wrong binary. Its plugin is named after
// the binary's file name, so that one build serves as two binaries.
package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"

	"github.com/Shougo/goda.vim/host"
	"github.com/Shougo/goda.vim/sdk"
)

type plugin struct{ name string }

func (p plugin) Name() string { return p.name }
func (plugin) Handlers() map[string]sdk.Handler {
	return map[string]sdk.Handler{
		"ping": func(context.Context, sdk.Host, []any) (any, error) { return "pong", nil },
	}
}

func main() {
	name := strings.ReplaceAll(filepath.Base(os.Args[0]), "-", "_")
	host.Main(plugin{name})
}

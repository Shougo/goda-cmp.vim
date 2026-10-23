// Package shell is a completion source for shell commands and their
// arguments as Vim completes them for ":!" (getcompletion()), for terminals
// and shell buffers; it works where the shell of ext/source/shellnative does
// not, on Windows. It is a port of ddc-source-shell
// (https://github.com/Shougo/ddc-source-shell).
package shell

import (
	"context"
	"fmt"

	"github.com/Shougo/goda-cmp.vim/cmp"
	"github.com/Shougo/goda-cmp.vim/ext/source/shellhistory"
	"github.com/Shougo/goda.vim/sdk"
)

type source struct{}

// New returns the source, named "shell".
func New() cmp.Source { return source{} }

func (source) Name() string { return "shell" }

func (source) Gather(ctx context.Context, h sdk.Host, c *cmp.Context) (cmp.Gathered, error) {
	// The file name before the cursor ('isfname').
	v, err := h.Call(ctx, "match", c.Input, `\f\+$`)
	if err != nil {
		return cmp.Gathered{}, err
	}
	start := int(toInt(v))
	if start < 0 {
		return cmp.Gathered{Start: len(c.Input)}, nil
	}
	input, err := shellhistory.ShellInput(ctx, h, c)
	if err != nil {
		return cmp.Gathered{}, err
	}
	var items []cmp.Item
	if v, err := h.Call(ctx, "getcompletion", "!"+input, "cmdline"); err == nil {
		if l, ok := v.([]any); ok {
			for _, x := range l {
				items = append(items, cmp.Item{Word: fmt.Sprint(x)})
			}
		}
	}
	return cmp.Gathered{Start: start, Items: items, Incomplete: true}, nil
}

func toInt(v any) int64 {
	switch n := v.(type) {
	case int64:
		return n
	case uint64:
		return int64(n)
	case int:
		return int64(n)
	case int8:
		return int64(n)
	case float64:
		return int64(n)
	}
	return -1
}

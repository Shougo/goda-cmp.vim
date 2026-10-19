package cmp

import (
	"context"

	"github.com/Shougo/goda.vim/sdk"
)

// NativeUI shows candidates with Vim's own popup menu (complete()). The
// Vim-side helpers are in autoload/goda_cmp.vim.
func NativeUI() UI {
	return vimUI{name: "native", show: "goda_cmp#_native_show", hide: "goda_cmp#_native_hide"}
}

// PumUI shows candidates with pum.vim, which must be installed. Its key
// mappings and options are configured in Vim script as usual
// (pum#set_option, pum#map#...).
func PumUI() UI { return vimUI{name: "pum", show: "goda_cmp#_pum_show", hide: "goda_cmp#_pum_hide"} }

// vimUI is a UI implemented by two Vim script functions.
type vimUI struct{ name, show, hide string }

func (u vimUI) Name() string { return u.name }

func (u vimUI) Show(ctx context.Context, h sdk.Host, r ShowRequest) error {
	_, err := h.Call(ctx, u.show, map[string]any{
		"seq":   r.Context.Seq,
		"tick":  r.Context.Tick,
		"col":   r.Col,
		"items": r.Items,
		// false while sources are still answering (see StreamSource)
		"final": r.Final,
	})
	return err
}

func (u vimUI) Hide(ctx context.Context, h sdk.Host) error {
	_, err := h.Call(ctx, u.hide)
	return err
}

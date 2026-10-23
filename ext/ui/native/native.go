// Package native is a UI that shows candidates with Vim's own popup menu
// (complete()). Its Vim script is autoload/goda_cmp/ui/native.vim.
package native

import (
	"context"

	"github.com/Shougo/goda-cmp.vim/cmp"
	"github.com/Shougo/goda.vim/sdk"
)

// Params are the parameters of the UI, as ddc-ui-native's.
type Params struct {
	// Insert inserts the first item when the popup opens, which suits
	// manual completion.
	Insert bool `json:"insert"`
}

type ui struct{ p Params }

// New returns the UI, named "native". The parameters may be given (at most
// one Params).
func New(p ...Params) cmp.UI {
	var u ui
	if len(p) > 0 {
		u.p = p[0]
	}
	return u
}

func (ui) Name() string { return "native" }

// WithParams sets the parameters from Vim script (uiParams).
func (u ui) WithParams(p cmp.Params) (any, error) {
	np, err := cmp.PatchParams(u.p, p)
	if err != nil {
		return nil, err
	}
	return ui{np}, nil
}

func (u ui) Show(ctx context.Context, h sdk.Host, r cmp.ShowRequest) error {
	_, err := h.Call(ctx, "goda_cmp#ui#native#show", map[string]any{
		"insert": u.p.Insert,
		"seq":    r.Context.Seq,
		"tick":   r.Context.Tick,
		"mode":   r.Context.Mode,
		"col":    r.Col,
		"items":  r.Items,
		// false while sources are still answering (see cmp.StreamSource)
		"final": r.Final,
	})
	return err
}

func (ui) Hide(ctx context.Context, h sdk.Host) error {
	_, err := h.Call(ctx, "goda_cmp#ui#native#hide")
	return err
}

// SkipCompletion: moving through the popup inserts the selected item, which
// Vim reports as TextChangedP while an item is selected.
func (ui) SkipCompletion(ctx context.Context, h sdk.Host, c *cmp.Context) (bool, error) {
	if c.Event != "TextChangedP" {
		return false, nil
	}
	v, err := h.Eval(ctx, "get(complete_info(['selected']), 'selected', -1)")
	if err != nil {
		return false, err
	}
	n, _ := v.(int64)
	return n >= 0, nil
}

// UpdateInfo puts the resolved Info into the info popup, which Vim has only
// when 'completeopt' has "popup" and the item had an info to begin with.
func (ui) UpdateInfo(ctx context.Context, h sdk.Host, it cmp.Item) error {
	if it.Info == "" {
		return nil
	}
	_, err := h.Call(ctx, "goda_cmp#ui#native#update_info", map[string]any{"ref": it.Ref, "info": it.Info})
	return err
}

// Visible: Vim's pumvisible().
func (ui) Visible(ctx context.Context, h sdk.Host) (bool, error) {
	v, err := h.Call(ctx, "pumvisible")
	if err != nil {
		return false, err
	}
	switch n := v.(type) {
	case int64:
		return n != 0, nil
	case uint64:
		return n != 0, nil
	case int8:
		return n != 0, nil
	case int:
		return n != 0, nil
	case bool:
		return n, nil
	}
	return false, nil
}

var (
	_ cmp.Skipper      = ui{}
	_ cmp.InfoUpdater  = ui{}
	_ cmp.Visibler     = ui{}
	_ cmp.Configurable = ui{}
)

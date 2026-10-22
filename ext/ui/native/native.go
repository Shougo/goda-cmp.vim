// Package native is a UI that shows candidates with Vim's own popup menu
// (complete()). Its Vim script is autoload/goda_cmp/ui/native.vim.
package native

import (
	"context"

	"github.com/Shougo/goda-cmp.vim/cmp"
	"github.com/Shougo/goda.vim/sdk"
)

type ui struct{}

// New returns the UI, named "native".
func New() cmp.UI { return ui{} }

func (ui) Name() string { return "native" }

func (ui) Show(ctx context.Context, h sdk.Host, r cmp.ShowRequest) error {
	_, err := h.Call(ctx, "goda_cmp#ui#native#show", map[string]any{
		"seq":   r.Context.Seq,
		"tick":  r.Context.Tick,
		"col":   r.Col,
		"items": r.Items,
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

var (
	_ cmp.Skipper     = ui{}
	_ cmp.InfoUpdater = ui{}
)

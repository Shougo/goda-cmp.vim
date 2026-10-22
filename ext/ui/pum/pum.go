// Package pum is a UI that shows candidates with pum.vim, which must be
// installed. Its key mappings and options are configured in Vim script as
// usual (pum#set_option, pum#map#...). Its Vim script is
// autoload/goda_cmp/ui/pum.vim.
package pum

import (
	"context"

	"github.com/Shougo/goda-cmp.vim/cmp"
	"github.com/Shougo/goda.vim/sdk"
)

type ui struct{}

// New returns the UI, named "pum".
func New() cmp.UI { return ui{} }

func (ui) Name() string { return "pum" }

func (ui) Show(ctx context.Context, h sdk.Host, r cmp.ShowRequest) error {
	_, err := h.Call(ctx, "goda_cmp#ui#pum#show", map[string]any{
		"seq":   r.Context.Seq,
		"tick":  r.Context.Tick,
		"col":   r.Col,
		"items": r.Items,
		"final": r.Final,
	})
	return err
}

func (ui) Hide(ctx context.Context, h sdk.Host) error {
	_, err := h.Call(ctx, "goda_cmp#ui#pum#hide")
	return err
}

// SkipCompletion asks pum.vim whether the change was its own (inserting
// the selected item).
func (ui) SkipCompletion(ctx context.Context, h sdk.Host, _ *cmp.Context) (bool, error) {
	v, err := h.Call(ctx, "goda_cmp#ui#pum#skip")
	if err != nil {
		return false, err
	}
	switch b := v.(type) {
	case bool:
		return b, nil
	case int64:
		return b != 0, nil
	}
	return false, nil
}

// UpdateInfo shows the resolved Info of the current item.
func (ui) UpdateInfo(ctx context.Context, h sdk.Host, it cmp.Item) error {
	if it.Info == "" {
		return nil
	}
	_, err := h.Call(ctx, "goda_cmp#ui#pum#update_info", map[string]any{"ref": it.Ref, "info": it.Info})
	return err
}

var (
	_ cmp.Skipper     = ui{}
	_ cmp.InfoUpdater = ui{}
)

// Package pum is a UI that shows candidates with pum.vim, which must be
// installed. It works on the command line and in terminals too
// (cmp.ModesUI). Its key mappings and options are configured
// in Vim script as usual (pum#set_option, pum#map#...). Its Vim script is
// autoload/goda_cmp/ui/pum.vim.
package pum

import (
	"context"

	"github.com/Shougo/goda-cmp.vim/cmp"
	"github.com/Shougo/goda.vim/sdk"
)

// Params are the parameters of the UI, as ddc-ui-pum's.
type Params struct {
	// Insert inserts the first item when the popup opens, which suits
	// manual completion.
	Insert bool `json:"insert"`
}

type ui struct{ p Params }

// New returns the UI, named "pum". The parameters may be given (at most one
// Params).
func New(p ...Params) cmp.UI {
	var u ui
	if len(p) > 0 {
		u.p = p[0]
	}
	return u
}

func (ui) Name() string { return "pum" }

// WithParams sets the parameters from Vim script (uiParams).
func (u ui) WithParams(p cmp.Params) (any, error) {
	np, err := cmp.PatchParams(u.p, p)
	if err != nil {
		return nil, err
	}
	return ui{np}, nil
}

func (u ui) Show(ctx context.Context, h sdk.Host, r cmp.ShowRequest) error {
	_, err := h.Call(ctx, "goda_cmp#ui#pum#show", map[string]any{
		"seq":    r.Context.Seq,
		"tick":   r.Context.Tick,
		"mode":   r.Context.Mode,
		"manual": r.Context.Manual,
		"col":    r.Col,
		"items":  r.Items,
		"final":  r.Final,
		"insert": u.p.Insert,
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
	return truthy(v), nil
}

// Visible asks pum.vim whether its popup is open.
func (ui) Visible(ctx context.Context, h sdk.Host) (bool, error) {
	v, err := h.Call(ctx, "goda_cmp#ui#pum#visible")
	if err != nil {
		return false, err
	}
	return truthy(v), nil
}

func truthy(v any) bool {
	switch b := v.(type) {
	case bool:
		return b
	case int64:
		return b != 0
	case int:
		return b != 0
	case uint64:
		return b != 0
	case int8:
		return b != 0
	}
	return false
}

// UpdateInfo shows the resolved Info of the current item.
func (ui) UpdateInfo(ctx context.Context, h sdk.Host, it cmp.Item) error {
	if it.Info == "" {
		return nil
	}
	_, err := h.Call(ctx, "goda_cmp#ui#pum#update_info", map[string]any{"ref": it.Ref, "info": it.Info})
	return err
}

// Modes: pum.vim opens in insert mode, on the command line and in
// terminals.
func (ui) Modes() []string { return []string{"i", "c", "t"} }

var (
	_ cmp.ModesUI      = ui{}
	_ cmp.Skipper      = ui{}
	_ cmp.InfoUpdater  = ui{}
	_ cmp.Visibler     = ui{}
	_ cmp.Configurable = ui{}
)

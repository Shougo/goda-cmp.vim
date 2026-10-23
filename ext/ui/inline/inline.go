// Package inline is a UI that shows the first candidate as virtual text after
// the cursor (a text property or a popup in Vim, an extmark or a floating
// window on Neovim's command line). It suits a DynamicUI that chooses it for
// a single candidate. goda_cmp#ui#inline#visible() tells whether it shows
// something, and goda_cmp#map#insert_item(0) inserts it: >vim
//
//	inoremap <expr> <Tab> goda_cmp#ui#inline#visible()
//	      \ ? goda_cmp#map#insert_item(0) : "\<Tab>"
//
// It is a port of ddc-ui-inline (https://github.com/Shougo/ddc-ui-inline); its
// Vim script is autoload/goda_cmp/ui/inline.vim.
package inline

import (
	"context"

	"github.com/Shougo/goda-cmp.vim/cmp"
	"github.com/Shougo/goda.vim/sdk"
)

// Params are the parameters of the UI, as ddc-ui-inline's.
type Params struct {
	// CheckNextWordMatched: when the word after the cursor ends the
	// candidate, show it only if it does at its end.
	CheckNextWordMatched bool `json:"checkNextWordMatched"`
	// Highlight is the highlight group of the text (default
	// "ComplMatchIns").
	Highlight string `json:"highlight"`
	// MaxWidth bounds the text shown (default 200).
	MaxWidth int `json:"maxWidth"`
}

type ui struct{ p Params }

// New returns the UI, named "inline". The parameters may be given (at most
// one Params).
func New(p ...Params) cmp.UI {
	var u ui
	if len(p) > 0 {
		u.p = p[0]
	}
	return ui{fill(u.p)}
}

func fill(p Params) Params {
	if p.Highlight == "" {
		p.Highlight = "ComplMatchIns"
	}
	if p.MaxWidth <= 0 {
		p.MaxWidth = 200
	}
	return p
}

func (ui) Name() string { return "inline" }

// WithParams sets the parameters from Vim script (uiParams).
func (u ui) WithParams(p cmp.Params) (any, error) {
	np, err := cmp.PatchParams(u.p, p)
	if err != nil {
		return nil, err
	}
	return ui{fill(np)}, nil
}

func (u ui) Show(ctx context.Context, h sdk.Host, r cmp.ShowRequest) error {
	_, err := h.Call(ctx, "goda_cmp#ui#inline#show", map[string]any{
		"seq":   r.Context.Seq,
		"tick":  r.Context.Tick,
		"mode":  r.Context.Mode,
		"col":   r.Col,
		"items": r.Items,
		"params": map[string]any{
			"checkNextWordMatched": u.p.CheckNextWordMatched,
			"highlight":            u.p.Highlight,
			"maxWidth":             u.p.MaxWidth,
		},
	})
	return err
}

func (ui) Hide(ctx context.Context, h sdk.Host) error {
	_, err := h.Call(ctx, "goda_cmp#ui#inline#hide")
	return err
}

// SkipCompletion: not while another popup (Vim's or pum.vim's) is open.
func (ui) SkipCompletion(ctx context.Context, h sdk.Host, _ *cmp.Context) (bool, error) {
	v, err := h.Eval(ctx, "pumvisible() || (exists('*pum#visible') && pum#visible())")
	if err != nil {
		return false, err
	}
	return truthy(v), nil
}

// Visible: goda_cmp#ui#inline#visible().
func (ui) Visible(ctx context.Context, h sdk.Host) (bool, error) {
	v, err := h.Call(ctx, "goda_cmp#ui#inline#visible")
	if err != nil {
		return false, err
	}
	return truthy(v), nil
}

// Modes: insert mode and the command line.
func (ui) Modes() []string { return []string{"i", "c"} }

func truthy(v any) bool {
	switch b := v.(type) {
	case bool:
		return b
	case int64:
		return b != 0
	case uint64:
		return b != 0
	case int:
		return b != 0
	case int8:
		return b != 0
	}
	return false
}

var (
	_ cmp.Skipper      = ui{}
	_ cmp.Visibler     = ui{}
	_ cmp.ModesUI      = ui{}
	_ cmp.Configurable = ui{}
)

package cmptest

import (
	"context"

	"github.com/Shougo/goda-cmp.vim/cmp"
	"github.com/Shougo/goda.vim/sdk"
)

// UI is a UI for tests. It calls Vim functions named after it,
// "goda_cmp_testui#{name}#show", "#hide", "#skip", "#visible" and
// "#update_info", which a Host fakes (or a test runtime defines, for a real
// editor). It has every optional interface of a UI.
type UI struct {
	name  string
	modes []string
	skip  bool
}

// NewUI returns a UI named name that can show candidates in modes ("i",
// "c", "t"; insert mode only when none is given).
func NewUI(name string, modes ...string) UI {
	if len(modes) == 0 {
		modes = []string{"i"}
	}
	return UI{name: name, modes: modes}
}

// WithSkip returns the UI that asks "skip" whether a change was its own
// (cmp.Skipper); without it, the answer is no and nothing is called.
func (u UI) WithSkip() UI {
	u.skip = true
	return u
}

func (u UI) Name() string { return u.name }

func (u UI) fn(f string) string { return "goda_cmp_testui#" + u.name + "#" + f }

// Show calls "show" with what to show: seq, tick, mode, manual, col, items
// and final.
func (u UI) Show(ctx context.Context, h sdk.Host, r cmp.ShowRequest) error {
	_, err := h.Call(ctx, u.fn("show"), map[string]any{
		"seq":    r.Context.Seq,
		"tick":   r.Context.Tick,
		"mode":   r.Context.Mode,
		"manual": r.Context.Manual,
		"col":    r.Col,
		"items":  r.Items,
		"final":  r.Final,
	})
	return err
}

func (u UI) Hide(ctx context.Context, h sdk.Host) error {
	_, err := h.Call(ctx, u.fn("hide"))
	return err
}

// SkipCompletion calls "skip" with the event of the request (see WithSkip).
func (u UI) SkipCompletion(ctx context.Context, h sdk.Host, c *cmp.Context) (bool, error) {
	if !u.skip {
		return false, nil
	}
	v, err := h.Call(ctx, u.fn("skip"), c.Event)
	if err != nil {
		return false, err
	}
	return truthy(v), nil
}

func (u UI) Visible(ctx context.Context, h sdk.Host) (bool, error) {
	v, err := h.Call(ctx, u.fn("visible"))
	if err != nil {
		return false, err
	}
	return truthy(v), nil
}

// UpdateInfo calls "update_info" with the ref and the info of the item.
func (u UI) UpdateInfo(ctx context.Context, h sdk.Host, it cmp.Item) error {
	_, err := h.Call(ctx, u.fn("update_info"), map[string]any{"ref": it.Ref, "info": it.Info})
	return err
}

func (u UI) Modes() []string { return u.modes }

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
	case float64:
		return b != 0
	}
	return false
}

var (
	_ cmp.ModesUI     = UI{}
	_ cmp.Skipper     = UI{}
	_ cmp.InfoUpdater = UI{}
	_ cmp.Visibler    = UI{}
)

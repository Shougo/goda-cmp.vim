// Package none is a UI that shows nothing, for configurations that only use
// the goda_cmp#map#*() functions (goda_cmp#map#insert_item(0), say). It is a
// port of ddc-ui-none (https://github.com/Shougo/ddc-ui-none).
package none

import (
	"context"

	"github.com/Shougo/goda-cmp.vim/cmp"
	"github.com/Shougo/goda.vim/sdk"
)

type ui struct{}

// New returns the UI, named "none".
func New() cmp.UI { return ui{} }

func (ui) Name() string                                          { return "none" }
func (ui) Show(context.Context, sdk.Host, cmp.ShowRequest) error { return nil }
func (ui) Hide(context.Context, sdk.Host) error                  { return nil }

// Visible: nothing is ever shown.
func (ui) Visible(context.Context, sdk.Host) (bool, error) { return false, nil }

// Modes: anywhere, since nothing is shown.
func (ui) Modes() []string { return []string{"i", "c", "t"} }

var (
	_ cmp.Visibler = ui{}
	_ cmp.ModesUI  = ui{}
)

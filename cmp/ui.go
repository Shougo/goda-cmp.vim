package cmp

import (
	"context"

	"github.com/Shougo/goda.vim/sdk"
)

// UIs are extensions like sources and filters (see ext/ui/native and
// ext/ui/pum): the engine knows none of them and calls no Vim script of its
// own to show candidates. A UI talks to the editor itself, through the Host
// it is given, and its Vim script talks back to the engine through the
// goda_cmp#_* receivers (see autoload/goda_cmp.vim).

// Skipper is implemented by UIs that insert text themselves, typically when
// the user moves through the list, and can tell such a change from typing.
// SkipCompletion runs before every automatic request (not for
// goda_cmp#complete()); true drops the request, so that the popup is not
// rebuilt under the user.
type Skipper interface {
	SkipCompletion(ctx context.Context, h sdk.Host, c *Context) (bool, error)
}

// ModesUI is implemented by UIs that can show candidates outside insert mode.
// Modes lists the modes (Context.Mode) the UI can show candidates in: "i"
// (insert mode), "c" (the command line, goda_cmp#enable_cmdline()) and "t" (a
// terminal, goda_cmp#enable_terminal()). A UI without it shows in insert mode
// only. Vim's own popup menu cannot do more (complete() works in insert mode
// only), pum.vim can. Command-line completion (Config.CmdlineSources) needs a
// UI with "c".
type ModesUI interface {
	Modes() []string
}

// uiModes is what UI u can show in.
func uiModes(u UI) []string {
	if m, ok := u.(ModesUI); ok {
		return m.Modes()
	}
	return []string{"i"}
}

// modeName is how errors name a mode.
func modeName(mode string) string {
	switch mode {
	case "c":
		return "on the command line"
	case "t":
		return "in a terminal"
	}
	return "in insert mode"
}

// InfoUpdater is implemented by UIs that can show what a source's Resolve
// added to the selected item (its Info). The engine calls it on the UI that
// showed the item; the item carries the Ref the UI gave it as user_data.
type InfoUpdater interface {
	UpdateInfo(ctx context.Context, h sdk.Host, it Item) error
}

// Visibler is implemented by UIs that can tell whether their popup is open,
// for goda_cmp#visible(). Without it the engine answers from what it showed
// and hid.
type Visibler interface {
	Visible(ctx context.Context, h sdk.Host) (bool, error)
}

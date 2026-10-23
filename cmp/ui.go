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

// CmdlineUI is implemented by UIs that can show candidates on the command
// line (Context.Mode "c"); ShowsCmdline reports whether this one does. Vim's
// own popup menu cannot (complete() works in insert mode only), pum.vim can.
// Command-line completion (Config.CmdlineSources) needs such a UI.
type CmdlineUI interface {
	ShowsCmdline() bool
}

// InfoUpdater is implemented by UIs that can show what a source's Resolve
// added to the selected item (its Info). The engine calls it on the UI that
// showed the item; the item carries the Ref the UI gave it as user_data.
type InfoUpdater interface {
	UpdateInfo(ctx context.Context, h sdk.Host, it Item) error
}

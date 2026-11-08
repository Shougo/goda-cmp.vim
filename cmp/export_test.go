package cmp

import "github.com/Shougo/goda.vim/sdk"

// WaitGathering waits for the completions of p that gather after their
// notification was handled, for a test to look at what they showed.
func WaitGathering(p sdk.Plugin) {
	if e, ok := p.(*engine); ok {
		e.gathering.Wait()
	}
}

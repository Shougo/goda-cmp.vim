// A configuration for the real-Vim test of buffer events: one source that
// wants BufEnter and BufDelete of text buffers and records them.
package main

import (
	"context"
	"fmt"
	"github.com/Shougo/goda-cmp.vim/ext/ui/native"
	"os"
	"path/filepath"

	"github.com/Shougo/goda-cmp.vim/cmp"
	"github.com/Shougo/goda.vim/sdk"
)

type recorder struct{}

func (recorder) Name() string { return "recorder" }
func (recorder) Gather(context.Context, sdk.Host, *cmp.Context) (cmp.Gathered, error) {
	return cmp.Gathered{}, nil
}
func (recorder) Events() []cmp.EventSpec {
	return []cmp.EventSpec{{Events: []string{"BufEnter", "BufDelete"}, Filetypes: []string{"text"}}}
}
func (recorder) OnEvent(_ context.Context, _ sdk.Host, ev cmp.Event) error {
	f, err := os.OpenFile(os.Getenv("GODA_EVENT_FILE"), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = fmt.Fprintf(f, "%s %s %s\n", ev.Name, ev.Filetype, filepath.Base(ev.Path))
	return err
}

func main() {
	b := cmp.New()
	b.AddUI(native.New())
	b.AddSource(recorder{})
	b.PatchGlobal(cmp.Config{UI: "native", Sources: []string{"recorder"}})
	b.Main()
}

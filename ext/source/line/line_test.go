package line

import (
	"context"
	"reflect"
	"testing"

	"github.com/Shougo/goda-cmp.vim/cmp"
	"github.com/Shougo/goda-cmp.vim/cmp/cmptest"
)

func TestGather(t *testing.T) {
	h := cmptest.Lines("func main() {", "\tfmt.Println()", "  ")
	g, err := New(Params{}).Gather(context.Background(), h, &cmp.Context{Bufnr: 1, Lnum: 3, Input: "  fm"})
	if err != nil {
		t.Fatal(err)
	}
	var ws []string
	for _, it := range g.Items {
		ws = append(ws, it.Word)
	}
	if g.Start != 2 || !reflect.DeepEqual(ws, []string{"func main() {", "fmt.Println()", ""}) {
		t.Errorf("start %d words %q", g.Start, ws)
	}
}

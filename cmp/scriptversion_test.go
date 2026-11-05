package cmp

import (
	"os"
	"regexp"
	"strconv"
	"testing"

	"github.com/Shougo/goda.vim/sdk"
)

// What a binary serves says its version.
var _ = []sdk.ScriptVersioner{(*engine)(nil), brokenPlugin{}}

// The Vim script says the version the binary does (sdk.ScriptVersioner).
func TestScriptVersion(t *testing.T) {
	b, err := os.ReadFile("../autoload/goda_cmp.vim")
	if err != nil {
		t.Fatal(err)
	}
	m := regexp.MustCompile(`(?m)^let s:version = #\{version: (\d+),`).FindSubmatch(b)
	if m == nil {
		t.Fatal("no s:version in autoload/goda_cmp.vim")
	}
	if v, _ := strconv.Atoi(string(m[1])); v != ScriptVersion {
		t.Errorf("s:version of autoload/goda_cmp.vim is %d, ScriptVersion is %d", v, ScriptVersion)
	}
}

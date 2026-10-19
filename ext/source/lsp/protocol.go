package lsp

import (
	"encoding/json"
	"strings"
)

// The subset of the Language Server Protocol (3.17) that completion needs.

type position struct {
	Line      int `json:"line"`
	Character int `json:"character"`
}

type lspRange struct {
	Start position `json:"start"`
	End   position `json:"end"`
}

type textEdit struct {
	Range   lspRange `json:"range"`
	NewText string   `json:"newText"`
}

// editRange is a TextEdit (Range) or an InsertReplaceEdit (Insert and
// Replace); textEdit and itemDefaults.editRange both come in these shapes.
// The insert range ends at the cursor, which is what complete() replaces;
// whatever reaches past the cursor is removed on accept.
type editRange struct {
	Range   *lspRange `json:"range"`   // TextEdit
	Insert  *lspRange `json:"insert"`  // InsertReplaceEdit
	Replace *lspRange `json:"replace"` // InsertReplaceEdit
	NewText string    `json:"newText"`
}

func (e editRange) insertRange() *lspRange {
	if e.Insert != nil {
		return e.Insert
	}
	return e.Range
}

// decodeEditRange reads itemDefaults.editRange: a bare Range or {insert,
// replace}.
func decodeEditRange(raw json.RawMessage) editRange {
	var e editRange
	if len(raw) == 0 || string(raw) == "null" {
		return e
	}
	if json.Unmarshal(raw, &e) == nil && e.Insert != nil {
		return e
	}
	var r lspRange
	if json.Unmarshal(raw, &r) == nil {
		return editRange{Range: &r}
	}
	return editRange{}
}

type completionItem struct {
	Label        string `json:"label"`
	LabelDetails *struct {
		Detail      string `json:"detail"`
		Description string `json:"description"`
	} `json:"labelDetails"`
	Kind                int             `json:"kind"`
	Detail              string          `json:"detail"`
	Documentation       json.RawMessage `json:"documentation"`
	SortText            string          `json:"sortText"`
	FilterText          string          `json:"filterText"`
	InsertText          string          `json:"insertText"`
	InsertTextFormat    int             `json:"insertTextFormat"`
	TextEdit            *editRange      `json:"textEdit"`
	TextEditText        string          `json:"textEditText"`
	AdditionalTextEdits []textEdit      `json:"additionalTextEdits"`
	Command             json.RawMessage `json:"command"`
}

const snippetFormat = 2

type itemDefaults struct {
	EditRange        json.RawMessage `json:"editRange"`
	InsertTextFormat int             `json:"insertTextFormat"`
	Data             json.RawMessage `json:"data"`
}

type completionList struct {
	IsIncomplete bool              `json:"isIncomplete"`
	ItemDefaults *itemDefaults     `json:"itemDefaults"`
	Items        []json.RawMessage `json:"items"`
}

// decodeCompletion reads a completion result: null, CompletionItem[] or a
// CompletionList. Items stay raw, since completionItem/resolve must get them
// back unchanged.
func decodeCompletion(raw json.RawMessage) (completionList, error) {
	var l completionList
	s := strings.TrimSpace(string(raw))
	switch {
	case s == "" || s == "null":
		return l, nil
	case strings.HasPrefix(s, "["):
		err := json.Unmarshal(raw, &l.Items)
		return l, err
	default:
		err := json.Unmarshal(raw, &l)
		return l, err
	}
}

// withDefaultData puts itemDefaults.data into an item that has none, so that
// the item is complete when it is sent back with completionItem/resolve.
func withDefaultData(raw json.RawMessage, data json.RawMessage) json.RawMessage {
	if len(data) == 0 {
		return raw
	}
	var m map[string]json.RawMessage
	if json.Unmarshal(raw, &m) != nil {
		return raw
	}
	if _, ok := m["data"]; ok {
		return raw
	}
	m["data"] = data
	b, err := json.Marshal(m)
	if err != nil {
		return raw
	}
	return b
}

// documentation reads a string or a MarkupContent.
func documentation(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return s
	}
	var mc struct {
		Value string `json:"value"`
	}
	if json.Unmarshal(raw, &mc) == nil {
		return mc.Value
	}
	return ""
}

var kindNames = [...]string{
	"", "Text", "Method", "Function", "Constructor", "Field", "Variable",
	"Class", "Interface", "Module", "Property", "Unit", "Value", "Enum",
	"Keyword", "Snippet", "Color", "File", "Reference", "Folder",
	"EnumMember", "Constant", "Struct", "Event", "Operator", "TypeParameter",
}

func kindName(k int) string {
	if k > 0 && k < len(kindNames) {
		return kindNames[k]
	}
	return ""
}

type serverCapabilities struct {
	PositionEncoding   string          `json:"positionEncoding"`
	TextDocumentSync   json.RawMessage `json:"textDocumentSync"`
	CompletionProvider *struct {
		TriggerCharacters []string `json:"triggerCharacters"`
		ResolveProvider   bool     `json:"resolveProvider"`
	} `json:"completionProvider"`
}

// syncKind is the server's TextDocumentSyncKind (0 none, 1 full,
// 2 incremental); either a number or an options object.
func (c serverCapabilities) syncKind() int {
	raw := c.TextDocumentSync
	if len(raw) == 0 {
		return 0
	}
	var n int
	if json.Unmarshal(raw, &n) == nil {
		return n
	}
	var o struct {
		Change    int  `json:"change"`
		OpenClose bool `json:"openClose"`
	}
	if json.Unmarshal(raw, &o) == nil {
		if o.Change == 0 && o.OpenClose {
			return 1 // open/close only: still worth opening the document
		}
		return o.Change
	}
	return 1
}

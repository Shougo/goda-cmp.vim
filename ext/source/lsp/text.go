package lsp

import (
	"net/url"
	"path/filepath"
	"strings"
	"unicode/utf8"
)

// Position encodings (LSP 3.17 positionEncoding). Vim works in bytes, so
// "utf-8" is preferred; "utf-16" is the protocol's default.
const (
	encUTF8  = "utf-8"
	encUTF16 = "utf-16"
	encUTF32 = "utf-32"
)

// units is the length of s in the code units of enc.
func units(s, enc string) int {
	switch enc {
	case encUTF8:
		return len(s)
	case encUTF32:
		return utf8.RuneCountInString(s)
	}
	n := 0
	for _, r := range s {
		if r >= 0x10000 {
			n += 2
		} else {
			n++
		}
	}
	return n
}

// byteOffset converts a character offset in the units of enc into a byte
// offset in line, clamped to the line.
func byteOffset(line string, char int, enc string) int {
	if char <= 0 {
		return 0
	}
	if enc == encUTF8 {
		return min(char, len(line))
	}
	n := 0
	for i, r := range line {
		if n >= char {
			return i
		}
		if enc == encUTF16 && r >= 0x10000 {
			n += 2
		} else {
			n++
		}
	}
	return len(line)
}

// fileURI turns an absolute path into a file:// URI. A Windows path
// (C:\dir\f.go) becomes file:///C:/dir/f.go.
func fileURI(path string) string {
	p := filepath.ToSlash(path)
	if !strings.HasPrefix(p, "/") {
		p = "/" + p
	}
	return (&url.URL{Scheme: "file", Path: p}).String()
}

// stripSnippet turns snippet syntax into the plain text it inserts by
// default: tab stops vanish, placeholders keep their default, choices keep
// the first one. Servers are told that snippets are not supported, but some
// send them anyway.
func stripSnippet(s string) string {
	var b strings.Builder
	stripInto(&b, s, false)
	return b.String()
}

// stripInto writes the text of s and returns how many bytes it consumed.
// Inside a placeholder (nested) it stops at the closing brace.
func stripInto(b *strings.Builder, s string, nested bool) int {
	i := 0
	for i < len(s) {
		c := s[i]
		switch {
		case c == '\\' && i+1 < len(s):
			b.WriteByte(s[i+1])
			i += 2
		case c == '}' && nested:
			return i
		case c == '$' && i+1 < len(s) && isDigit(s[i+1]):
			i++
			for i < len(s) && isDigit(s[i]) {
				i++
			}
		case c == '$' && i+1 < len(s) && s[i+1] == '{':
			i += 2
			for i < len(s) && (isDigit(s[i]) || s[i] == '_' || isAlpha(s[i])) {
				i++
			}
			if i < len(s) && s[i] == ':' {
				i++
				i += stripInto(b, s[i:], true)
			} else if i < len(s) && s[i] == '|' {
				i++
				end := strings.IndexAny(s[i:], ",|")
				if end < 0 {
					end = len(s) - i
				}
				b.WriteString(s[i : i+end])
				if close := strings.Index(s[i:], "|}"); close >= 0 {
					i += close + 1
				} else {
					i = len(s)
				}
			}
			if i < len(s) && s[i] == '}' {
				i++
			}
		default:
			b.WriteByte(c)
			i++
		}
	}
	return i
}

func isDigit(c byte) bool { return c >= '0' && c <= '9' }
func isAlpha(c byte) bool { return c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' }

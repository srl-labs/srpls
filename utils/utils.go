package utils

import (
	"strings"

	goyang "github.com/openconfig/goyang/pkg/yang"
	protocol "github.com/tliron/glsp/protocol_3_16"
)

func StripQuotes(s string) string {
	if len(s) > 0 && s[0] == '"' {
		s = s[1:]
	}
	if len(s) > 0 && s[len(s)-1] == '"' {
		s = s[:len(s)-1]
	}
	return s
}

func StripQuotesWithOffsets(s string) (string, int, int) {
	left, right := 0, 0
	if len(s) > 0 && s[0] == '"' {
		s = s[1:]
		left = 1
	}
	if len(s) > 0 && s[len(s)-1] == '"' {
		s = s[:len(s)-1]
		right = 1
	}
	return s, left, right
}

func DiagRange(line, start, end uint32) protocol.Range {
	return protocol.Range{
		Start: protocol.Position{Line: line, Character: start},
		End:   protocol.Position{Line: line, Character: end},
	}
}

func NumberInRange(n goyang.Number, yr goyang.YangRange) bool {
	for _, r := range yr {
		if !n.Less(r.Min) && !r.Max.Less(n) {
			return true
		}
	}
	return false
}

func SafePosition(line, token string) (uint32, uint32) {
	if token == "" {
		idx := strings.Index(line, `""`)
		if idx >= 0 {
			return uint32(idx), uint32(idx + 2)
		}
		return 0, uint32(len(line))
	}
	idx := strings.Index(line, token)
	if idx < 0 {
		return 0, uint32(len(line))
	}
	return uint32(idx), uint32(idx + len(token))
}

func IsWordBoundary(b byte) bool {
	return b == ' ' || b == '\t' || b == '{' || b == '}'
}

func LineIndent(line string) int {
	return len(line) - len(strings.TrimLeft(line, " \t"))
}

func StrPtr(s string) *string { return &s }

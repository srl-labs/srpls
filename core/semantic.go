package core

import (
	"strings"

	"github.com/srl-labs/srpls/yang"

	goyang "github.com/openconfig/goyang/pkg/yang"
	"github.com/tliron/glsp"
	protocol "github.com/tliron/glsp/protocol_3_16"
)

const (
	tokenContainer = iota
	tokenList
	tokenLeaf
	tokenKey
	tokenValue
)

// map the YANG constructs to vscode semantic token types
var SemanticTokenTypes = []string{
	"keyword",  // container
	"type",     // list
	"property", // leaf
	"string",   // key
	"number",   // value
}

func TextDocumentSemanticTokensFull(_ *glsp.Context, params *protocol.SemanticTokensParams) (*protocol.SemanticTokens, error) {
	empty := &protocol.SemanticTokens{Data: []uint32{}}
	doc := NewDocumentContext(params.TextDocument.URI)
	if doc == nil || doc.Model == nil {
		return empty, nil
	}

	lines := strings.Split(doc.Content, "\n")
	format := doc.Lang.DetectFormat(doc.Content)

	var enc tokenEncoder

	for lineNum, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || trimmed == "}" {
			continue
		}

		if doc.Lang.IsComment(trimmed) {
			continue
		}

		switch format {
		case FormatFlat:
			encodeFlatLine(&enc, doc.Model, line, uint32(lineNum))
		case FormatBrace:
			encodeBraceLine(&enc, doc.Model, doc.Content, line, uint32(lineNum))
		}
	}

	return &protocol.SemanticTokens{Data: enc.data}, nil
}

func encodeFlatLine(enc *tokenEncoder, model *yang.Model, line string, lineNum uint32) {
	trimmed := strings.TrimSpace(line)
	if !strings.HasPrefix(trimmed, "/") {
		return
	}

	slashPos := uint32(strings.Index(line, "/"))
	rest := line[slashPos+1:]
	offset := slashPos + 1

	children := yang.FlattenChoices(model.Root)

	for rest != "" {
		rest = strings.TrimLeft(rest, " \t")
		offset = uint32(len(line)) - uint32(len(rest))
		if rest == "" {
			break
		}

		var tok string
		var tokLen uint32
		if rest[0] == '"' {
			end := strings.Index(rest[1:], "\"")
			if end >= 0 {
				tok = rest[1 : end+1]
				tokLen = uint32(end + 2)
			} else {
				break
			}
		} else {
			end := strings.IndexAny(rest, " \t")
			if end < 0 {
				tok = rest
				tokLen = uint32(len(rest))
			} else {
				tok = rest[:end]
				tokLen = uint32(end)
			}
		}

		entry, ok := children[tok]
		if !ok {
			enc.add(lineNum, offset, tokLen, tokenValue)
			rest = rest[tokLen:]
			offset += tokLen
			continue
		}

		switch {
		case entry.Kind == goyang.DirectoryEntry && entry.IsList():
			enc.add(lineNum, offset, tokLen, tokenList)
			rest = rest[tokLen:]
			offset += tokLen

			rest = strings.TrimLeft(rest, " \t")
			offset = uint32(len(line)) - uint32(len(rest))
			if rest == "" {
				break
			}
			var keyLen uint32
			if rest[0] == '"' {
				end := strings.Index(rest[1:], "\"")
				if end >= 0 {
					keyLen = uint32(end + 2)
				} else {
					break
				}
			} else {
				end := strings.IndexAny(rest, " \t")
				if end < 0 {
					keyLen = uint32(len(rest))
				} else {
					keyLen = uint32(end)
				}
			}
			enc.add(lineNum, offset, keyLen, tokenKey)
			rest = rest[keyLen:]
			offset += keyLen

			if entry.Dir != nil {
				children = yang.FlattenChoices(entry.Dir)
			}

		case entry.Kind == goyang.DirectoryEntry:
			enc.add(lineNum, offset, tokLen, tokenContainer)
			rest = rest[tokLen:]
			offset += tokLen
			if entry.Dir != nil {
				children = yang.FlattenChoices(entry.Dir)
			}

		case entry.Kind == goyang.LeafEntry:
			enc.add(lineNum, offset, tokLen, tokenLeaf)
			rest = rest[tokLen:]
			offset += tokLen

			val := strings.TrimSpace(rest)
			if val != "" {
				valOffset := uint32(len(line)) - uint32(len(rest)) + uint32(strings.Index(rest, val))
				enc.add(lineNum, valOffset, uint32(len(val)), tokenValue)
			}
			return
		}
	}
}

func encodeBraceLine(enc *tokenEncoder, model *yang.Model, content, line string, lineNum uint32) {
	parsed := parseBraceDocumentForSemantic(content, line, lineNum)
	if parsed == nil {
		return
	}
	pl := *parsed

	children := yang.FlattenChoices(model.Root)

	for i := 0; i < pl.ParentDepth && i < len(pl.PathTokens); i++ {
		entry, ok := children[pl.PathTokens[i]]
		if !ok {
			break
		}
		switch {
		case entry.Kind == goyang.DirectoryEntry && entry.IsList():
			i++
			if entry.Dir != nil {
				children = yang.FlattenChoices(entry.Dir)
			}
		case entry.Kind == goyang.DirectoryEntry:
			if entry.Dir != nil {
				children = yang.FlattenChoices(entry.Dir)
			}
		}
	}

	// Colorize the tokens visible on this line.
	trimmed := strings.TrimSpace(line)
	trimmed = strings.TrimSuffix(trimmed, "{")
	trimmed = strings.TrimSpace(trimmed)
	if trimmed == "" {
		return
	}

	offset := uint32(strings.Index(line, strings.TrimSpace(line)))
	rest := line[offset:]

	lineTokens := pl.PathTokens[pl.ParentDepth:]
	for i := 0; i < len(lineTokens); i++ {
		tok := lineTokens[i]
		rest = strings.TrimLeft(rest, " \t")
		offset = uint32(len(line)) - uint32(len(rest))

		var tokLen uint32
		if rest != "" && rest[0] == '"' {
			end := strings.Index(rest[1:], "\"")
			if end >= 0 {
				tokLen = uint32(end + 2)
			} else {
				break
			}
		} else {
			idx := strings.Index(rest, tok)
			if idx < 0 {
				break
			}
			offset += uint32(idx)
			rest = rest[idx:]
			tokLen = uint32(len(tok))
		}

		entry, ok := children[tok]
		if !ok {
			enc.add(lineNum, offset, tokLen, tokenValue)
			rest = rest[tokLen:]
			continue
		}

		switch {
		case entry.Kind == goyang.DirectoryEntry && entry.IsList():
			enc.add(lineNum, offset, tokLen, tokenList)
			rest = rest[tokLen:]

			// next token is key
			if i+1 < len(lineTokens) {
				i++
				rest = strings.TrimLeft(rest, " \t")
				offset = uint32(len(line)) - uint32(len(rest))
				var keyLen uint32
				if rest != "" && rest[0] == '"' {
					end := strings.Index(rest[1:], "\"")
					if end >= 0 {
						keyLen = uint32(end + 2)
					}
				} else {
					end := strings.IndexAny(rest, " \t{")
					if end < 0 {
						keyLen = uint32(len(rest))
					} else {
						keyLen = uint32(end)
					}
				}
				if keyLen > 0 {
					enc.add(lineNum, offset, keyLen, tokenKey)
					rest = rest[keyLen:]
				}
			}
			if entry.Dir != nil {
				children = yang.FlattenChoices(entry.Dir)
			}

		case entry.Kind == goyang.DirectoryEntry:
			enc.add(lineNum, offset, tokLen, tokenContainer)
			rest = rest[tokLen:]
			if entry.Dir != nil {
				children = yang.FlattenChoices(entry.Dir)
			}

		case entry.Kind == goyang.LeafEntry:
			enc.add(lineNum, offset, tokLen, tokenLeaf)
			rest = rest[tokLen:]
			val := strings.TrimSpace(rest)
			val = strings.TrimSuffix(val, "{")
			val = strings.TrimSpace(val)
			if val != "" {
				valOffset := uint32(len(line)) - uint32(len(rest)) + uint32(strings.Index(rest, val))
				enc.add(lineNum, valOffset, uint32(len(val)), tokenValue)
			}
			return
		}
	}
}

func parseBraceDocumentForSemantic(content, line string, lineNum uint32) *ParsedLine {
	d := &DefaultLanguage{}
	parsed := d.ParseBraceDocument(content)
	pl, ok := parsed[int(lineNum)]
	if !ok {
		return nil
	}
	return &pl
}

type tokenEncoder struct {
	data     []uint32
	prevLine uint32
	prevChar uint32
}

func (e *tokenEncoder) add(line, start, length uint32, tokenType int) {
	deltaLine := line - e.prevLine
	var deltaStart uint32
	if deltaLine == 0 {
		deltaStart = start - e.prevChar
	} else {
		deltaStart = start
	}
	e.prevLine = line
	e.prevChar = start
	e.data = append(e.data, deltaLine, deltaStart, length, uint32(tokenType), 0)
}

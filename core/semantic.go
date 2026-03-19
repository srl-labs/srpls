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
	tokenString
	tokenComment
)

// map the YANG constructs to vscode semantic token types
var SemanticTokenTypes = []string{
	"keyword",  // container
	"type",     // list
	"property", // leaf
	"string",   // key
	"number",   // value
	"string",   // string (multi-line)
	"comment",  // comment
}

func TextDocumentSemanticTokensFull(_ *glsp.Context, params *protocol.SemanticTokensParams) (*protocol.SemanticTokens, error) {
	empty := &protocol.SemanticTokens{Data: []uint32{}}
	doc := NewDocumentContext(params.TextDocument.URI)
	if doc == nil || doc.Model == nil {
		return empty, nil
	}

	lines := strings.Split(doc.Content, "\n")
	format := doc.Lang.DetectFormat(doc.Content)

	var parsed map[int]ParsedLine
	if format == FormatBrace {
		if sap, ok := doc.Lang.(SchemaAwareParser); ok && doc.Model != nil {
			parsed = sap.ParseDocumentWithModel(doc.Content, doc.Model)
		} else {
			parsed = doc.Lang.ParseDocument(doc.Content)
		}
	}

	var enc tokenEncoder

	for lineNum := 0; lineNum < len(lines); lineNum++ {
		line := lines[lineNum]
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || trimmed == "}" || trimmed == "]" {
			continue
		}

		if doc.Lang.IsComment(trimmed) {
			enc.add(uint32(lineNum), uint32(strings.Index(line, strings.TrimSpace(line))), uint32(len(trimmed)), tokenComment)
			continue
		}

		// Leaf-list values in [ ... ] — emit keyword for the leaf name
		if strings.HasSuffix(trimmed, "[") {
			leafName := strings.TrimSpace(strings.TrimSuffix(trimmed, "["))
			if leafName != "" {
				leafOffset := uint32(strings.Index(line, leafName))
				enc.add(uint32(lineNum), leafOffset, uint32(len(leafName)), tokenLeaf)
			}
			for lineNum++; lineNum < len(lines); lineNum++ {
				inner := strings.TrimSpace(lines[lineNum])
				if inner == "]" {
					break
				}
				if inner != "" {
					offset := uint32(strings.Index(lines[lineNum], inner))
					enc.add(uint32(lineNum), offset, uint32(len(inner)), tokenValue)
				}
			}
			continue
		}

		// Multi-line strings — encode path tokens normally, then string for content
		if strings.Count(trimmed, "\"")%2 != 0 {
			quoteIdx := strings.Index(line, "\"")
			if quoteIdx > 0 {
				// Encode the path portion before the quote using the normal encoder
				pathLine := line[:quoteIdx]
				switch format {
				case FormatFlat:
					encodeFlatLine(&enc, doc.Model, pathLine, uint32(lineNum))
				case FormatBrace:
					if pl, ok := parsed[lineNum]; ok {
						encodeBraceLine(&enc, doc.Model, parsed, pathLine, uint32(lineNum))
						_ = pl
					}
				}
				// Emit the quoted string portion
				enc.add(uint32(lineNum), uint32(quoteIdx), uint32(len(line)-quoteIdx), tokenString)
			}
			for lineNum++; lineNum < len(lines); lineNum++ {
				innerLine := lines[lineNum]
				if strings.Contains(innerLine, "\"") {
					inner := strings.TrimRight(innerLine, " \t")
					if len(inner) > 0 {
						enc.add(uint32(lineNum), 0, uint32(len(inner)), tokenString)
					}
					break
				}
				inner := strings.TrimRight(innerLine, " \t")
				if len(inner) > 0 {
					enc.add(uint32(lineNum), 0, uint32(len(inner)), tokenString)
				}
			}
			continue
		}

		switch format {
		case FormatFlat:
			encodeFlatLine(&enc, doc.Model, line, uint32(lineNum))
		case FormatBrace:
			encodeBraceLine(&enc, doc.Model, parsed, line, uint32(lineNum))
		}
	}

	return &protocol.SemanticTokens{Data: enc.data}, nil
}

func encodeFlatLine(enc *tokenEncoder, model *yang.Model, line string, lineNum uint32) {
	trimmed := strings.TrimSpace(line)

	var slashPos int
	if strings.HasPrefix(trimmed, "set / ") {
		slashPos = strings.Index(line, "set / ") + 4
	} else if strings.HasPrefix(trimmed, "/") {
		slashPos = strings.Index(line, "/")
	} else {
		return
	}

	rest := line[slashPos+1:]
	var offset uint32

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

		// Inline leaf-list: [ val1 val2 ... ]
		if tok == "[" {
			enc.add(lineNum, offset, 1, tokenValue)
			rest = rest[1:]
			for rest != "" {
				rest = strings.TrimLeft(rest, " \t")
				offset = uint32(len(line)) - uint32(len(rest))
				if rest == "" {
					break
				}
				end := strings.IndexAny(rest, " \t")
				var valTok string
				var valLen uint32
				if end < 0 {
					valTok = rest
					valLen = uint32(len(rest))
				} else {
					valTok = rest[:end]
					valLen = uint32(end)
				}
				if valTok == "]" {
					enc.add(lineNum, offset, 1, tokenValue)
					break
				}
				enc.add(lineNum, offset, valLen, tokenValue)
				rest = rest[valLen:]
			}
			return
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

			// Color all key tokens (compound keys have multiple)
			keySkip := listKeyTokenCount(entry)
			for k := 0; k < keySkip; k++ {
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
			if val != "" {
				valOffset := uint32(len(line)) - uint32(len(rest)) + uint32(strings.Index(rest, val))
				enc.add(lineNum, valOffset, uint32(len(val)), tokenValue)
			}
			return
		}
	}
}

func encodeBraceLine(enc *tokenEncoder, model *yang.Model, parsed map[int]ParsedLine, line string, lineNum uint32) {
	pl, ok := parsed[int(lineNum)]
	if !ok {
		return
	}

	children := yang.FlattenChoices(model.Root)

	for i := 0; i < pl.ParentDepth && i < len(pl.PathTokens); i++ {
		entry, ok := children[pl.PathTokens[i]]
		if !ok {
			break
		}
		switch {
		case entry.Kind == goyang.DirectoryEntry && entry.IsList():
			i += listKeyTokenCount(entry)
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

			// colorize all key tokens (compound keys have multiple)
			keySkip := listKeyTokenCount(entry)
			for k := 0; k < keySkip && i+1 < len(lineTokens); k++ {
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

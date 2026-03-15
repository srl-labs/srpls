package core

import (
	"fmt"
	"strings"

	"github.com/srl-labs/srpls/utils"
	"github.com/srl-labs/srpls/yang"

	goyang "github.com/openconfig/goyang/pkg/yang"
	"github.com/tliron/glsp"
	protocol "github.com/tliron/glsp/protocol_3_16"
)

func TextDocumentHover(_ *glsp.Context, params *protocol.HoverParams) (*protocol.Hover, error) {
	doc := NewDocumentContext(params.TextDocument.URI)
	if doc == nil || doc.Model == nil {
		return nil, nil
	}

	parsed := doc.Lang.ParseDocument(doc.Content)
	pl, ok := parsed[int(params.Position.Line)]
	if !ok {
		return nil, nil
	}

	word, wordStart, wordEnd := wordAtPosition(pl.LineText, params.Position.Character)
	if word == "" {
		return nil, nil
	}

	line := params.Position.Line
	entry := resolveHoverEntry(doc, pl, word)

	if entry == nil {
		return nil, nil
	}

	if entry.Kind == goyang.LeafEntry && pl.LeafValue == word {
		return hoverForLeafValue(entry, line, wordStart, wordEnd), nil
	}

	return hoverForEntry(entry, line, wordStart, wordEnd), nil
}

func resolveHoverEntry(doc *DocumentContext, pl ParsedLine, word string) *goyang.Entry {
	children := yang.FlattenChoices(doc.Model.Root)
	tokens := pl.PathTokens

	var lastEntry *goyang.Entry
	i := 0
	for i < len(tokens) {
		entry, ok := children[tokens[i]]
		if !ok {
			break
		}
		lastEntry = entry

		if tokens[i] == word {
			return entry
		}
		i++

		switch {
		case entry.Kind == goyang.DirectoryEntry && entry.IsList():
			if i < len(tokens) {
				i++
			}
			if entry.Dir != nil {
				children = yang.FlattenChoices(entry.Dir)
			} else {
				return entry
			}

		case entry.Kind == goyang.DirectoryEntry:
			if entry.Dir != nil {
				children = yang.FlattenChoices(entry.Dir)
			} else {
				return entry
			}

		case entry.Kind == goyang.LeafEntry:
			return entry
		}
	}

	return lastEntry
}

func hoverForEntry(entry *goyang.Entry, line, start, end uint32) *protocol.Hover {
	var parts []string

	kind := "container"
	switch {
	case entry.Kind == goyang.DirectoryEntry && entry.IsList():
		kind = "list"
	case entry.Kind == goyang.LeafEntry:
		kind = "leaf"
		if entry.Type != nil {
			kind = fmt.Sprintf("leaf (%s)", yangTypeName(entry.Type))
		}
	}
	parts = append(parts, fmt.Sprintf("**%s** — *%s*", entry.Name, kind))

	if entry.Description != "" {
		parts = append(parts, entry.Description)
	}

	content := strings.Join(parts, "\n\n")
	rng := protocol.Range{
		Start: protocol.Position{Line: line, Character: start},
		End:   protocol.Position{Line: line, Character: end},
	}
	return &protocol.Hover{
		Contents: protocol.MarkupContent{
			Kind:  protocol.MarkupKindMarkdown,
			Value: content,
		},
		Range: &rng,
	}
}

func hoverForLeafValue(entry *goyang.Entry, line, start, end uint32) *protocol.Hover {
	if entry.Type == nil {
		return nil
	}

	header := fmt.Sprintf("Value for **%s**", entry.Name)
	typeLine := fmt.Sprintf("Type: *%s*", yangTypeName(entry.Type))

	var parts []string
	parts = append(parts, header, typeLine)

	if entry.Description != "" {
		parts = append(parts, entry.Description)
	}

	content := strings.Join(parts, "\n\n")
	rng := protocol.Range{
		Start: protocol.Position{Line: line, Character: start},
		End:   protocol.Position{Line: line, Character: end},
	}
	return &protocol.Hover{
		Contents: protocol.MarkupContent{
			Kind:  protocol.MarkupKindMarkdown,
			Value: content,
		},
		Range: &rng,
	}
}

func yangTypeName(t *goyang.YangType) string {
	if t.Name != "" {
		return t.Name
	}
	return t.Kind.String()
}

func wordAtPosition(line string, pos uint32) (string, uint32, uint32) {
	if int(pos) > len(line) {
		pos = uint32(len(line))
	}

	start := int(pos)
	for start > 0 && !utils.IsWordBoundary(line[start-1]) {
		start--
	}

	end := int(pos)
	for end < len(line) && !utils.IsWordBoundary(line[end]) {
		end++
	}

	if start == end {
		return "", 0, 0
	}

	word, trimLeft, trimRight := utils.StripQuotesWithOffsets(line[start:end])
	start += trimLeft
	end -= trimRight

	return word, uint32(start), uint32(end)
}


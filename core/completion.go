package core

import (
	"fmt"
	"sort"
	"strings"

	"github.com/srl-labs/srpls/utils"
	"github.com/srl-labs/srpls/yang"

	goyang "github.com/openconfig/goyang/pkg/yang"
	"github.com/tliron/glsp"
	protocol "github.com/tliron/glsp/protocol_3_16"
)

func TextDocumentCompletion(_ *glsp.Context, params *protocol.CompletionParams) (any, error) {
	doc := NewDocumentContext(params.TextDocument.URI)
	if doc == nil || doc.Model == nil {
		return nil, nil
	}

	completer, ok := doc.Lang.(Completer)
	if !ok {
		return nil, nil
	}

	cc := completer.ContextAtCursor(doc.Content, params.Position.Line, params.Position.Character)
	prefixRange := protocol.Range{
		Start: protocol.Position{
			Line:      params.Position.Line,
			Character: params.Position.Character - uint32(len(cc.Prefix)),
		},
		End: params.Position,
	}

	children := yang.FlattenChoices(doc.Model.Root)
	children, lastEntry := walkYangPath(children, cc.ParentPath)
	if lastEntry != nil && lastEntry.Kind == goyang.LeafEntry {
		return nil, nil
	}
	if children == nil {
		return nil, nil
	}

	lineChildren, lineEntry := walkYangPath(children, cc.LineTokens)

	var hints []string
	if hinter, ok := doc.Lang.(ValueHinter); ok && lineEntry != nil {
		hints = hinter.ValueHints(schemaPath(lineEntry))
	}

	if cc.Format == FormatFlat && !cc.HasLeader && len(cc.LineTokens) == 0 && len(cc.ParentPath) == 0 && cc.Prefix == "" {
		kind := protocol.CompletionItemKindOperator
		return []protocol.CompletionItem{{
			Label:    "/",
			Kind:     &kind,
			TextEdit: makeTextEdit(prefixRange, "/"),
		}}, nil
	}

	switch {
	case lineEntry != nil && lineEntry.Kind == goyang.LeafEntry:
		return valueCompletions(lineEntry, cc.Prefix, prefixRange, hints), nil

	case lineEntry != nil && lineEntry.Kind == goyang.DirectoryEntry && lineEntry.IsList() && lineChildren == nil:
		return listKeyHint(lineEntry, cc.Prefix, prefixRange, hints), nil

	case lineChildren != nil:
		return keywordCompletions(lineChildren, cc.Prefix, prefixRange, cc.Format, cc.Indent), nil

	default:
		return keywordCompletions(children, cc.Prefix, prefixRange, cc.Format, cc.Indent), nil
	}
}

func walkYangPath(start map[string]*goyang.Entry, tokens []string) (map[string]*goyang.Entry, *goyang.Entry) {
	current := start
	var lastEntry *goyang.Entry

	i := 0
	for i < len(tokens) {
		entry, ok := current[tokens[i]]
		if !ok {
			return current, lastEntry
		}
		lastEntry = entry
		i++

		switch {
		case entry.Kind == goyang.DirectoryEntry && entry.IsList():
			if i >= len(tokens) {
				return nil, entry
			}
			i++ // skip the list key value
			if entry.Dir != nil {
				current = yang.FlattenChoices(entry.Dir)
			} else {
				return nil, entry
			}

		case entry.Kind == goyang.DirectoryEntry:
			if entry.Dir != nil {
				current = yang.FlattenChoices(entry.Dir)
			} else {
				return nil, entry
			}

		case entry.Kind == goyang.LeafEntry:
			return nil, entry
		}
	}
	return current, lastEntry
}

func keywordCompletions(children map[string]*goyang.Entry, prefix string, rng protocol.Range, format ConfigFormat, indent string) []protocol.CompletionItem {
	var items []protocol.CompletionItem
	snippetFormat := protocol.InsertTextFormatSnippet

	for name, entry := range children {
		if prefix != "" && !strings.HasPrefix(name, prefix) {
			continue
		}

		kind := protocol.CompletionItemKindProperty
		insertText := name + " "
		var insertFormat *protocol.InsertTextFormat

		switch {
		case entry.Kind == goyang.DirectoryEntry && entry.IsList():
			kind = protocol.CompletionItemKindClass
		case entry.Kind == goyang.DirectoryEntry:
			kind = protocol.CompletionItemKindModule
			if format == FormatBrace {
				insertText = name + " {\n" + indent + "\t$0\n" + indent + "}"
				insertFormat = &snippetFormat
			}
		}

		item := protocol.CompletionItem{
			Label:            name,
			Kind:             &kind,
			InsertTextFormat: insertFormat,
			TextEdit:         makeTextEdit(rng, insertText),
		}
		if entry.Description != "" {
			item.Detail = &entry.Description
		}
		items = append(items, item)
	}

	sort.Slice(items, func(i, j int) bool {
		return items[i].Label < items[j].Label
	})
	return items
}

func valueCompletions(entry *goyang.Entry, prefix string, rng protocol.Range, hints []string) []protocol.CompletionItem {
	leafIsString := entry.Type != nil && isStringType(entry.Type)
	items := hintCompletions(hints, prefix, rng, leafIsString)

	if entry.Type != nil {
		items = append(items, typeCompletions(entry.Type, prefix, rng)...)
	}
	return items
}

func listKeyHint(entry *goyang.Entry, prefix string, rng protocol.Range, hints []string) []protocol.CompletionItem {
	keyName := entry.Key
	if keyName == "" {
		keyName = "key"
	}

	keyIsString := false
	var keyLeaf *goyang.Entry
	if entry.Dir != nil {
		if kl, ok := entry.Dir[keyName]; ok {
			keyLeaf = kl
			keyIsString = kl.Type != nil && isStringType(kl.Type)
		}
	}

	items := hintCompletions(hints, prefix, rng, keyIsString)

	label := "<" + keyName + ">"
	if prefix == "" || strings.HasPrefix(label, prefix) {
		kind := protocol.CompletionItemKindSnippet
		detail := "List key: " + keyName
		insertText := ""

		if keyLeaf != nil {
			if keyLeaf.Description != "" {
				detail = keyLeaf.Description
			}
			if keyIsString {
				insertText = "\"\" "
			}
		}

		items = append(items, protocol.CompletionItem{
			Label:      label,
			Kind:       &kind,
			Detail:     &detail,
			FilterText: &label,
			TextEdit:   makeTextEdit(rng, insertText),
			SortText:   utils.StrPtr("1"),
		})
	}

	return items
}

func hintCompletions(hints []string, prefix string, rng protocol.Range, quoteStrings bool) []protocol.CompletionItem {
	var items []protocol.CompletionItem
	for i, h := range hints {
		insertText := h + " "
		if quoteStrings {
			insertText = "\"" + h + "\" "
		}
		if prefix != "" && !strings.HasPrefix(h, prefix) && !strings.HasPrefix("\""+h, prefix) {
			continue
		}
		kind := protocol.CompletionItemKindValue
		items = append(items, protocol.CompletionItem{
			Label:    h,
			Kind:     &kind,
			TextEdit: makeTextEdit(rng, insertText),
			SortText: utils.StrPtr(fmt.Sprintf("0%d", i)),
		})
	}
	return items
}

func typeCompletions(t *goyang.YangType, prefix string, rng protocol.Range) []protocol.CompletionItem {
	switch t.Kind {
	case goyang.Yenum:
		if t.Enum != nil {
			return filterNames(t.Enum.Names(), prefix, protocol.CompletionItemKindEnumMember, rng)
		}

	case goyang.Ybool:
		return filterNames([]string{"true", "false"}, prefix, protocol.CompletionItemKindValue, rng)

	case goyang.Yidentityref:
		if t.Enum != nil {
			return filterNames(t.Enum.Names(), prefix, protocol.CompletionItemKindEnumMember, rng)
		}

	case goyang.Yunion:
		seen := make(map[string]bool)
		var items []protocol.CompletionItem
		for _, member := range t.Type {
			for _, item := range typeCompletions(member, prefix, rng) {
				if !seen[item.Label] {
					seen[item.Label] = true
					items = append(items, item)
				}
			}
		}
		return items
	}

	if hint := typeHintItem(t, prefix, rng); hint != nil {
		return []protocol.CompletionItem{*hint}
	}
	return nil
}

func typeHintItem(t *goyang.YangType, prefix string, rng protocol.Range) *protocol.CompletionItem {
	name := typeHintLabel(t)
	if name == "" {
		return nil
	}
	label := "<" + name + ">"
	if prefix != "" && !strings.HasPrefix(label, prefix) {
		return nil
	}

	kind := protocol.CompletionItemKindSnippet
	detail := "Type: " + name
	insertText := ""
	if isStringType(t) {
		insertText = "\"\" "
	}

	return &protocol.CompletionItem{
		Label:      label,
		Kind:       &kind,
		Detail:     &detail,
		FilterText: &label,
		TextEdit:   makeTextEdit(rng, insertText),
		SortText:   utils.StrPtr("0"),
	}
}

func typeHintLabel(t *goyang.YangType) string {
	if t.Name != "" && t.Name != t.Kind.String() {
		return t.Name
	}
	switch t.Kind {
	case goyang.Ystring:
		return "string"
	case goyang.Yint8:
		return "int8"
	case goyang.Yint16:
		return "int16"
	case goyang.Yint32:
		return "int32"
	case goyang.Yint64:
		return "int64"
	case goyang.Yuint8:
		return "uint8"
	case goyang.Yuint16:
		return "uint16"
	case goyang.Yuint32:
		return "uint32"
	case goyang.Yuint64:
		return "uint64"
	case goyang.Ydecimal64:
		return "decimal64"
	case goyang.Ybinary:
		return "binary"
	case goyang.Yleafref:
		return "leafref"
	case goyang.Yempty:
		return "empty"
	}
	return ""
}

func isStringType(t *goyang.YangType) bool {
	if t.Kind == goyang.Ystring {
		return true
	}
	if t.Kind == goyang.Yunion {
		for _, member := range t.Type {
			if isStringType(member) {
				return true
			}
		}
	}
	return false
}

func filterNames(names []string, prefix string, kind protocol.CompletionItemKind, rng protocol.Range) []protocol.CompletionItem {
	var items []protocol.CompletionItem
	for _, name := range names {
		if prefix != "" && !strings.HasPrefix(name, prefix) {
			continue
		}
		items = append(items, protocol.CompletionItem{
			Label:    name,
			Kind:     &kind,
			TextEdit: makeTextEdit(rng, name),
		})
	}
	return items
}

func schemaPath(entry *goyang.Entry) string {
	var parts []string
	for e := entry; e != nil; e = e.Parent {
		if e.Name != "" && e.Parent != nil {
			parts = append([]string{e.Name}, parts...)
		}
	}
	return strings.Join(parts, "/")
}

func makeTextEdit(rng protocol.Range, newText string) protocol.TextEdit {
	return protocol.TextEdit{Range: rng, NewText: newText}
}

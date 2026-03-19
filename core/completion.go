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
	var hintDetail string
	if hinter, ok := doc.Lang.(ValueHinter); ok && lineEntry != nil {
		sp := schemaPath(lineEntry)
		hints = hinter.ValueHints(sp, doc.Content)
		hintDetail = hinter.HintDetail(sp, doc.Content)
	}

	quoteListKeys := true
	if q, ok := doc.Lang.(ListKeyQuoter); ok {
		quoteListKeys = q.QuotesListKeys()
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
		return valueCompletions(lineEntry, cc.Prefix, prefixRange, hints, hintDetail), nil

	case lineEntry != nil && lineEntry.Kind == goyang.DirectoryEntry && lineEntry.IsList() && lineChildren == nil:
		return listKeyHint(lineEntry, cc.LineTokens, cc.Prefix, prefixRange, hints, quoteListKeys, hintDetail), nil

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
			// skip key tokens: first key is 1 token (value only),
			// each additional key is 2 tokens (keyword + value)
			skip := listKeyTokenCount(entry)
			if i+skip > len(tokens) {
				// not enough tokens to fill all keys — still need more key values
				return nil, entry
			}
			i += skip
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

func valueCompletions(entry *goyang.Entry, prefix string, rng protocol.Range, hints []string, hintDetail string) []protocol.CompletionItem {
	leafIsString := entry.Type != nil && isStringType(entry.Type)
	items := hintCompletions(hints, prefix, rng, leafIsString, hintDetail, entry.Description)

	if entry.Type != nil {
		items = append(items, typeCompletions(entry.Type, prefix, rng)...)
	}
	return items
}

func listKeyHint(entry *goyang.Entry, lineTokens []string, prefix string, rng protocol.Range, hints []string, quoteListKeys bool, hintDetail string) []protocol.CompletionItem {
	keys := strings.Fields(entry.Key)
	if len(keys) == 0 {
		keys = []string{"key"}
	}

	// Figure out which key we're completing based on tokens after the list name.
	// Tokens: [listName, key1Val, key2Keyword, key2Val, ...]
	// After listName: first key = 1 token, each additional = 2 tokens
	tokensAfterName := 0
	for i, t := range lineTokens {
		if t == entry.Name {
			tokensAfterName = len(lineTokens) - i - 1
			break
		}
	}

	// Determine current key index from consumed tokens
	keyIdx := 0
	consumed := 0
	for k := range keys {
		need := 1
		if k > 0 {
			need = 2
		}
		if consumed+need > tokensAfterName {
			keyIdx = k
			break
		}
		consumed += need
		keyIdx = k + 1
	}

	// If we've consumed tokens for a key keyword but not its value,
	// check if we're on the keyword or the value position
	onKeyword := false
	if keyIdx > 0 && keyIdx < len(keys) {
		// For non-first keys, tokens are: keyword, value
		// If consumed tokens place us at the keyword position, suggest the keyword
		tokensForThisKey := tokensAfterName - consumed
		if tokensForThisKey == 0 {
			onKeyword = true
		}
	}

	if keyIdx >= len(keys) {
		// All keys filled — shouldn't reach here
		return nil
	}

	keyName := keys[keyIdx]

	// If we need to suggest the keyword for a non-first key
	if onKeyword && keyIdx > 0 {
		kind := protocol.CompletionItemKindKeyword
		if prefix == "" || strings.HasPrefix(keyName, prefix) {
			return []protocol.CompletionItem{{
				Label:    keyName,
				Kind:     &kind,
				TextEdit: makeTextEdit(rng, keyName+" "),
			}}
		}
		return nil
	}

	// Suggest values for the current key
	keyIsString := false
	var keyLeaf *goyang.Entry
	if entry.Dir != nil {
		if kl, ok := entry.Dir[keyName]; ok {
			keyLeaf = kl
			keyIsString = quoteListKeys && kl.Type != nil && isStringType(kl.Type)
		}
	}

	var keyDoc string
	if keyLeaf != nil {
		keyDoc = keyLeaf.Description
	}

	// Only use hints for the first key
	var keyHints []string
	var keyHintDetail string
	if keyIdx == 0 {
		keyHints = hints
		keyHintDetail = hintDetail
	}
	items := hintCompletions(keyHints, prefix, rng, keyIsString, keyHintDetail, keyDoc)

	// Add enum/type completions from the key leaf
	if keyLeaf != nil && keyLeaf.Type != nil {
		items = append(items, typeCompletions(keyLeaf.Type, prefix, rng)...)
	}

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

func hintCompletions(hints []string, prefix string, rng protocol.Range, quoteStrings bool, detail string, doc string) []protocol.CompletionItem {
	var items []protocol.CompletionItem
	var detailPtr *string
	if detail != "" {
		detailPtr = &detail
	}
	var docPtr *protocol.MarkupContent
	if doc != "" {
		docPtr = &protocol.MarkupContent{Kind: protocol.MarkupKindMarkdown, Value: doc}
	}
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
			Label:         h,
			Kind:          &kind,
			Detail:        detailPtr,
			Documentation: docPtr,
			TextEdit:      makeTextEdit(rng, insertText),
			SortText:      utils.StrPtr(fmt.Sprintf("0%d", i)),
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

// listKeyTokenCount returns how many tokens a list's key values occupy
// in the brace config format. The first key is value-only (1 token),
// each additional key is keyword + value (2 tokens each).
func listKeyTokenCount(entry *goyang.Entry) int {
	numKeys := len(strings.Fields(entry.Key))
	if numKeys <= 1 {
		return 1
	}
	return 1 + (numKeys-1)*2
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

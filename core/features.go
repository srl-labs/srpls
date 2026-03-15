package core

import (
	"github.com/tliron/glsp"
	protocol "github.com/tliron/glsp/protocol_3_16"
)

func TextDocumentDocumentSymbol(_ *glsp.Context, params *protocol.DocumentSymbolParams) (any, error) {
	doc := NewDocumentContext(params.TextDocument.URI)
	if doc == nil {
		return nil, nil
	}
	if sp, ok := doc.Lang.(SymbolProvider); ok {
		return sp.DocumentSymbols(doc.Content, doc.Model), nil
	}
	return nil, nil
}

func TextDocumentFoldingRange(_ *glsp.Context, params *protocol.FoldingRangeParams) ([]protocol.FoldingRange, error) {
	doc := NewDocumentContext(params.TextDocument.URI)
	if doc == nil {
		return nil, nil
	}
	if fp, ok := doc.Lang.(FoldingProvider); ok {
		return fp.FoldingRanges(doc.Content), nil
	}
	return nil, nil
}

func TextDocumentFormatting(_ *glsp.Context, params *protocol.DocumentFormattingParams) ([]protocol.TextEdit, error) {
	doc := NewDocumentContext(params.TextDocument.URI)
	if doc == nil {
		return nil, nil
	}
	if f, ok := doc.Lang.(Formatter); ok {
		return f.FormatDocument(doc.Content, params.Options), nil
	}
	return nil, nil
}

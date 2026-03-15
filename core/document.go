package core

import "github.com/srl-labs/srpls/yang"

type DocumentContext struct {
	URI     string
	Content string
	Lang    Language
	Version string
	Model   *yang.Model
}

func NewDocumentContext(uri string) *DocumentContext {
	content, ok := documentStore[uri]
	if !ok {
		return nil
	}
	lang := documentLangs[uri]
	if lang == nil {
		return nil
	}
	version := DocumentVersions[uri]
	return &DocumentContext{
		URI:     uri,
		Content: content,
		Lang:    lang,
		Version: version,
		Model:   GetYangModel(lang, version),
	}
}

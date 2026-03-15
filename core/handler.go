package core

import (
	"os"

	"github.com/srl-labs/srpls/yang"

	"github.com/tliron/commonlog"
	"github.com/tliron/glsp"
	protocol "github.com/tliron/glsp/protocol_3_16"
)

var documentStore = make(map[string]string)
var documentLangs = make(map[string]Language)

var DocumentVersions = make(map[string]string)

func detectAndHandleVersion(ctx *glsp.Context, uri, content string, l Language) {
	vd, ok := l.(VersionDetector)
	if !ok {
		return
	}

	version := vd.DetectVersion(content)

	// fallback to default if nothin detected
	if version == "" {
		if dvp, ok := l.(DefaultVersionProvider); ok {
			version = dvp.GetDefaultVersion()
		}
	}

	if version == "" {
		ctx.Notify("srpls/modelsNotFound", map[string]string{
			"uri":     uri,
			"version": "",
		})
		return
	}

	prev := DocumentVersions[uri]
	if prev == version {
		scheduler.schedule(ctx, uri, content, l, version)
		return
	}

	resolver, ok := l.(ModelPathResolver)
	if !ok {
		return
	}
	yangDir := resolver.YangDirForVersion(version)
	if info, err := os.Stat(yangDir); err == nil && info.IsDir() {
		loadYangModel(ctx, uri, version, l, yangDir)
		ctx.Notify("srpls/versionDetected", map[string]string{
			"uri":     uri,
			"version": version,
		})
	} else {
		ctx.Notify("srpls/modelsNotFound", map[string]string{
			"uri":     uri,
			"version": version,
		})
		ctx.Notify("srpls/versionDetected", map[string]string{
			"uri":     uri,
			"version": version,
		})
	}
}

func TextDocumentDidOpen(ctx *glsp.Context, params *protocol.DidOpenTextDocumentParams) error {
	uri := params.TextDocument.URI
	content := params.TextDocument.Text
	documentStore[uri] = content

	l := GetLanguage(params.TextDocument.LanguageID)
	if l == nil {
		return nil
	}
	documentLangs[uri] = l

	detectAndHandleVersion(ctx, uri, content, l)
	return nil
}

func TextDocumentDidChange(ctx *glsp.Context, params *protocol.DidChangeTextDocumentParams) error {
	uri := params.TextDocument.URI
	for _, change := range params.ContentChanges {
		if c, ok := change.(protocol.TextDocumentContentChangeEventWhole); ok {
			documentStore[uri] = c.Text
		}
	}

	l := documentLangs[uri]
	if l == nil {
		return nil
	}
	content, ok := documentStore[uri]
	if !ok {
		return nil
	}

	detectAndHandleVersion(ctx, uri, content, l)
	return nil
}

func TextDocumentDidClose(ctx *glsp.Context, params *protocol.DidCloseTextDocumentParams) error {
	uri := params.TextDocument.URI
	scheduler.cancel(uri)
	delete(documentStore, uri)
	delete(documentLangs, uri)
	delete(DocumentVersions, uri)
	ctx.Notify(protocol.ServerTextDocumentPublishDiagnostics, protocol.PublishDiagnosticsParams{
		URI:         uri,
		Diagnostics: []protocol.Diagnostic{},
	})
	return nil
}

func loadYangModel(ctx *glsp.Context, uri, version string, l Language, yangDir string) {
	log := commonlog.GetLogger(AppName)

	if GetYangModel(l, version) == nil {
		log.Infof("loading YANG models (version %s) from %s", version, yangDir)
		ym, err := yang.Load(yangDir, l.SkipDirs(), l.RootModules())
		if err != nil {
			log.Warningf("failed to load YANG models: %v", err)
			return
		}
		SetYangModel(l.Name(), version, ym)
		log.Infof("loaded %d top-level schema nodes for %s %s", len(ym.Root), l.Name(), version)
	}

	DocumentVersions[uri] = version

	content := documentStore[uri]
	if content != "" {
		scheduler.schedule(ctx, uri, content, l, version)
	}
}

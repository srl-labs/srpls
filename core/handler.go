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
var documentPlatforms = make(map[string]string)

func detectPlatform(content string, l Language) string {
	type platformDetector interface {
		DetectPlatform(content string) string
	}
	if pd, ok := l.(platformDetector); ok {
		return pd.DetectPlatform(content)
	}
	return ""
}

func notifyFormat(ctx *glsp.Context, uri, content string, l Language) {
	formatStr := "flat"
	if l.DetectFormat(content) == FormatBrace {
		formatStr = "brace"
	}
	ctx.Notify("srpls/formatDetected", map[string]string{
		"uri":    uri,
		"format": formatStr,
	})
}

func detectAndHandleVersion(ctx *glsp.Context, uri, content string, l Language) {
	vd, ok := l.(VersionDetector)
	if !ok {
		return
	}

	version := vd.DetectVersion(content)
	if version == "" {
		if dvp, ok := l.(DefaultVersionProvider); ok {
			version = dvp.GetDefaultVersion()
		}
	}
	if version == "" {
		if lr, ok := l.(LatestVersionResolver); ok {
			if v, _, found := lr.FindLatestVersion(); found {
				version = v
			}
		}
	}
	if version == "" {
		return
	}

	platform := detectPlatform(content, l)

	format := l.DetectFormat(content)

	if DocumentVersions[uri] == version {
		scheduler.schedule(ctx, uri, content, l, version)
		if platform != documentPlatforms[uri] {
			documentPlatforms[uri] = platform
			notifyVersion(ctx, uri, version, platform, format, true)
		}
		return
	}

	resolver, ok := l.(ModelPathResolver)
	if !ok {
		return
	}
	documentPlatforms[uri] = platform

	var yangDir, fallbackVer string
	exactDir := resolver.YangDirForVersion(version)
	if info, err := os.Stat(exactDir); err == nil && info.IsDir() {
		yangDir = exactDir
	} else if fb, ok := l.(FallbackResolver); ok {
		if fv, fd, found := fb.FindFallbackVersion(version); found {
			yangDir = fd
			fallbackVer = fv
		}
	}

	if yangDir != "" {
		loadYangModel(ctx, uri, version, l, yangDir)
		notifyVersion(ctx, uri, version, platform, format, true, fallbackVer)
	} else {
		notifyVersion(ctx, uri, version, platform, format, false)
	}

	if yangDir != exactDir {
		ctx.Notify("srpls/modelsNotFound", map[string]string{
			"uri":     uri,
			"version": version,
		})
	}
}

func notifyVersion(ctx *glsp.Context, uri, version, platform string, format ConfigFormat, modelsLoaded bool, loadedVersion ...string) {
	formatStr := "brace"
	if format == FormatFlat {
		formatStr = "flat"
	}
	params := map[string]any{
		"uri":          uri,
		"version":      version,
		"platform":     platform,
		"format":       formatStr,
		"modelsLoaded": modelsLoaded,
	}
	if len(loadedVersion) > 0 && loadedVersion[0] != "" {
		params["loadedVersion"] = loadedVersion[0]
	}
	ctx.Notify("srpls/versionDetected", params)
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

	notifyFormat(ctx, uri, content, l)
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

	notifyFormat(ctx, uri, content, l)
	detectAndHandleVersion(ctx, uri, content, l)
	return nil
}

func TextDocumentDidClose(ctx *glsp.Context, params *protocol.DidCloseTextDocumentParams) error {
	uri := params.TextDocument.URI
	scheduler.cancel(uri)
	delete(documentStore, uri)
	delete(documentLangs, uri)
	delete(DocumentVersions, uri)
	delete(documentPlatforms, uri)
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

	// loaded models, ask for a token re-request from the client
	ctx.Notify("workspace/semanticTokens/refresh", nil)

	content := documentStore[uri]
	if content != "" {
		scheduler.schedule(ctx, uri, content, l, version)
	}
}

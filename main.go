package main

import (
	"github.com/srl-labs/srpls/core"
	_ "github.com/srl-labs/srpls/srlinux"
	_ "github.com/srl-labs/srpls/sros"

	"github.com/tliron/commonlog"
	_ "github.com/tliron/commonlog/simple"
	"github.com/tliron/glsp"
	protocol "github.com/tliron/glsp/protocol_3_16"
	glspserver "github.com/tliron/glsp/server"
)

var (
	version    string = "0.1.0"
	stdHandler protocol.Handler
)

func main() {
	core.AppName = "Nokia SR Language Server"
	commonlog.Configure(1, nil)

	stdHandler = protocol.Handler{
		Initialize:                     initialize,
		Initialized:                    initialized,
		Shutdown:                       shutdown,
		SetTrace:                       setTrace,
		CancelRequest:                  func(_ *glsp.Context, _ *protocol.CancelParams) error { return nil },
		TextDocumentDidOpen:            core.TextDocumentDidOpen,
		TextDocumentDidChange:          core.TextDocumentDidChange,
		TextDocumentDidClose:           core.TextDocumentDidClose,
		TextDocumentCompletion:         core.TextDocumentCompletion,
		TextDocumentHover:              core.TextDocumentHover,
		TextDocumentDocumentSymbol:     core.TextDocumentDocumentSymbol,
		TextDocumentFoldingRange:       core.TextDocumentFoldingRange,
		TextDocumentFormatting:         core.TextDocumentFormatting,
		TextDocumentDidSave:            func(_ *glsp.Context, _ *protocol.DidSaveTextDocumentParams) error { return nil },
		WorkspaceDidChangeWatchedFiles: func(_ *glsp.Context, _ *protocol.DidChangeWatchedFilesParams) error { return nil },
	}

	s := glspserver.NewServer(&stdHandler, core.AppName, false)
	_ = s.RunStdio()
}

func initialize(_ *glsp.Context, _ *protocol.InitializeParams) (any, error) {
	capabilities := stdHandler.CreateServerCapabilities()
	syncKind := protocol.TextDocumentSyncKindFull
	capabilities.TextDocumentSync = &syncKind
	capabilities.CompletionProvider = &protocol.CompletionOptions{}
	capabilities.HoverProvider = true

	return protocol.InitializeResult{
		Capabilities: capabilities,
		ServerInfo: &protocol.InitializeResultServerInfo{
			Name:    core.AppName,
			Version: &version,
		},
	}, nil
}

func initialized(_ *glsp.Context, _ *protocol.InitializedParams) error { return nil }

func shutdown(_ *glsp.Context) error {
	protocol.SetTraceValue(protocol.TraceValueOff)
	return nil
}

func setTrace(_ *glsp.Context, params *protocol.SetTraceParams) error {
	protocol.SetTraceValue(params.Value)
	return nil
}

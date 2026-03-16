package core

import (
	"context"
	"strings"
	"sync"
	"time"

	"github.com/srl-labs/srpls/yang"

	"github.com/tliron/glsp"
	protocol "github.com/tliron/glsp/protocol_3_16"
)

const diagnosticDebounce = 200 * time.Millisecond

type diagScheduler struct {
	mu      sync.Mutex
	cancels map[string]context.CancelFunc
	gens    map[string]uint64
}

var scheduler = &diagScheduler{
	cancels: make(map[string]context.CancelFunc),
	gens:    make(map[string]uint64),
}

func (ds *diagScheduler) schedule(ctx *glsp.Context, uri string, content string, n Language, version string) {
	ds.mu.Lock()
	if cancel, ok := ds.cancels[uri]; ok {
		cancel()
	}
	ds.gens[uri]++
	gen := ds.gens[uri]
	bgCtx, cancel := context.WithCancel(context.Background())
	ds.cancels[uri] = cancel
	ds.mu.Unlock()

	go ds.run(bgCtx, ctx, uri, content, n, version, gen)
}

// run performs debounced validation in a background goroutine.
func (ds *diagScheduler) run(bgCtx context.Context, ctx *glsp.Context, uri, content string, n Language, version string, gen uint64) {
	select {
	case <-time.After(diagnosticDebounce):
	case <-bgCtx.Done():
		return
	}

	diagnostics := validateDocument(bgCtx, content, n, GetYangModel(n, version))
	if bgCtx.Err() != nil {
		return
	}

	ctx.Notify(protocol.ServerTextDocumentPublishDiagnostics, protocol.PublishDiagnosticsParams{
		URI:         uri,
		Diagnostics: diagnostics,
	})

	ds.mu.Lock()
	if ds.gens[uri] == gen {
		delete(ds.cancels, uri)
		delete(ds.gens, uri)
	}
	ds.mu.Unlock()
}

func (ds *diagScheduler) cancel(uri string) {
	ds.mu.Lock()
	if cancel, ok := ds.cancels[uri]; ok {
		cancel()
		delete(ds.cancels, uri)
		delete(ds.gens, uri)
	}
	ds.mu.Unlock()
}

func validateDocument(ctx context.Context, content string, n Language, ym *yang.Model) []protocol.Diagnostic {
	diagnostics := []protocol.Diagnostic{}

	if dv, ok := n.(DocumentValidator); ok {
		docDiags := dv.ValidateDocument(content)
		if len(docDiags) > 0 {
			return docDiags
		}
	}

	if ym == nil {
		return diagnostics
	}

	var parsed map[int]ParsedLine
	if sap, ok := n.(SchemaAwareParser); ok {
		parsed = sap.ParseDocumentWithModel(content, ym)
	} else {
		parsed = n.ParseDocument(content)
	}
	lines := strings.Split(content, "\n")

	for lineNum := 0; lineNum < len(lines); lineNum++ {
		if ctx.Err() != nil {
			return []protocol.Diagnostic{}
		}
		pl, ok := parsed[lineNum]
		if !ok {
			continue
		}
		diags := n.ValidateLine(pl, uint32(lineNum), ym, content)
		diagnostics = append(diagnostics, diags...)
	}
	return diagnostics
}

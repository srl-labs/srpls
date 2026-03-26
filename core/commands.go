package core

import (
	"fmt"
	"slices"
	"sort"
	"strings"

	"github.com/srl-labs/srpls/yang"

	"github.com/tliron/glsp"
	protocol "github.com/tliron/glsp/protocol_3_16"
)

// CommandConvert is the LSP executeCommand name for format conversion.
const CommandConvert = "srpls.convert"

// CommandKnownPlatforms is the LSP executeCommand name for retrieving known platforms.
const CommandKnownPlatforms = "srpls.knownPlatforms"

// CommandSetVersion is the LSP executeCommand name for setting the version directive.
const CommandSetVersion = "srpls.setVersion"

// CommandSetPlatform is the LSP executeCommand name for setting the platform directive.
const CommandSetPlatform = "srpls.setPlatform"

// CommandReloadVersion is the LSP executeCommand name for re-evaluating version after model download.
const CommandReloadVersion = "srpls.reloadVersion"

// CommandSetDefaultVersion sets the fallback version used when no version is detected from the document.
const CommandSetDefaultVersion = "srpls.setDefaultVersion"

const CommandNearestVersion = "srpls.nearestVersion"

// CommandGotoPath is the LSP executeCommand name for navigating to a path in the document.
const CommandGotoPath = "srpls.gotoPath"

// Converter is implemented by languages that support format conversion.
type Converter interface {
	Flatten(content string) string
	Unflatten(content string) string
	UnflattenWithModel(content string, ym *yang.Model) string
}

// WorkspaceExecuteCommand handles workspace/executeCommand requests.
func WorkspaceExecuteCommand(ctx *glsp.Context, params *protocol.ExecuteCommandParams) (any, error) {
	switch params.Command {
	case CommandConvert:
		return handleConvert(params.Arguments)
	case CommandKnownPlatforms:
		return handleKnownPlatforms()
	case CommandSetVersion:
		return handleSetDirective(params.Arguments, func(he HeaderEditor, content, value string) string {
			return he.SetVersionDirective(content, value)
		})
	case CommandSetPlatform:
		return handleSetDirective(params.Arguments, func(he HeaderEditor, content, value string) string {
			return he.SetPlatformDirective(content, value)
		})
	case CommandReloadVersion:
		return handleReloadVersion(ctx, params.Arguments)
	case CommandSetDefaultVersion:
		return handleSetDefaultVersion(ctx, params.Arguments)
	case CommandNearestVersion:
		return handleNearestVersion(params.Arguments)
	case CommandGotoPath:
		return handleGotoPath(params.Arguments)
	}
	return nil, fmt.Errorf("unknown command: %s", params.Command)
}

func handleKnownPlatforms() (any, error) {
	for _, l := range Registry {
		if pp, ok := l.(PlatformProvider); ok {
			return pp.KnownPlatforms(), nil
		}
	}
	return []string{}, nil
}

func handleSetDirective(args []any, apply func(HeaderEditor, string, string) string) (any, error) {
	if len(args) < 2 {
		return nil, fmt.Errorf("requires [uri, value]")
	}
	uri, ok := args[0].(string)
	if !ok {
		return nil, fmt.Errorf("first argument must be a URI string")
	}
	value, ok := args[1].(string)
	if !ok {
		return nil, fmt.Errorf("second argument must be a value string")
	}

	lang := documentLangs[uri]
	if lang == nil {
		return nil, fmt.Errorf("no language found for URI")
	}
	he, ok := lang.(HeaderEditor)
	if !ok {
		return nil, fmt.Errorf("language does not support header editing")
	}

	content := documentStore[uri]
	return apply(he, content, value), nil
}

func handleReloadVersion(ctx *glsp.Context, args []any) (any, error) {
	if len(args) < 1 {
		return nil, fmt.Errorf("reloadVersion requires [uri]")
	}
	uri, ok := args[0].(string)
	if !ok {
		return nil, fmt.Errorf("first argument must be a URI string")
	}

	l := documentLangs[uri]
	if l == nil {
		return nil, fmt.Errorf("no language found for URI")
	}

	delete(DocumentVersions, uri)

	content := documentStore[uri]
	if content != "" {
		detectAndHandleVersion(ctx, uri, content, l)
	}
	return nil, nil
}

func handleSetDefaultVersion(ctx *glsp.Context, args []any) (any, error) {
	if len(args) < 1 {
		return nil, fmt.Errorf("setDefaultVersion requires [version]")
	}
	version, ok := args[0].(string)
	if !ok {
		return nil, fmt.Errorf("first argument must be a version string")
	}
	for _, l := range Registry {
		if dl, ok := l.(interface{ SetDefaultVersion(string) }); ok {
			dl.SetDefaultVersion(version)
		}
	}

	for uri, content := range documentStore {
		if DocumentVersions[uri] != "" {
			continue
		}
		l := documentLangs[uri]
		if l == nil {
			continue
		}
		detectAndHandleVersion(ctx, uri, content, l)
	}
	return nil, nil
}

func handleNearestVersion(args []any) (any, error) {
	if len(args) < 2 {
		return nil, fmt.Errorf("nearestVersion requires [requested, candidates]")
	}
	requested, ok := args[0].(string)
	if !ok {
		return nil, fmt.Errorf("first argument must be a version string")
	}
	rawCandidates, ok := args[1].([]any)
	if !ok {
		return nil, fmt.Errorf("second argument must be an array of version strings")
	}
	candidates := make([]string, 0, len(rawCandidates))
	for _, v := range rawCandidates {
		if s, ok := v.(string); ok {
			candidates = append(candidates, s)
		}
	}
	return findNearestVersion(requested, candidates), nil
}

func handleConvert(args []any) (any, error) {
	if len(args) < 2 {
		return nil, fmt.Errorf("convert requires [uri, content]")
	}

	uri, ok := args[0].(string)
	if !ok {
		return nil, fmt.Errorf("first argument must be a URI string")
	}
	content, ok := args[1].(string)
	if !ok {
		return nil, fmt.Errorf("second argument must be content string")
	}

	cursorLine := -1
	if len(args) >= 3 {
		if cl, ok := args[2].(float64); ok {
			cursorLine = int(cl)
		}
	}

	lang := documentLangs[uri]
	if lang == nil {
		return nil, fmt.Errorf("no language found for URI")
	}

	conv, ok := lang.(Converter)
	if !ok {
		return nil, fmt.Errorf("language does not support conversion")
	}

	format := lang.DetectFormat(content)
	version := DocumentVersions[uri]
	ym := GetYangModel(lang, version)

	// Get path tokens at cursor line before conversion
	var cursorPath []string
	if cursorLine >= 0 {
		var parsed map[int]ParsedLine
		if sap, ok := lang.(SchemaAwareParser); ok && ym != nil {
			parsed = sap.ParseDocumentWithModel(content, ym)
		} else {
			parsed = lang.ParseDocument(content)
		}
		for line := cursorLine; line >= 0; line-- {
			if pl, ok := parsed[line]; ok {
				cursorPath = pl.PathTokens
				break
			}
		}
	}

	var newContent string
	switch format {
	case FormatFlat:
		if ym != nil {
			newContent = conv.UnflattenWithModel(content, ym)
		} else {
			newContent = conv.Unflatten(content)
		}

	case FormatBrace:
		newContent = conv.Flatten(content)

	default:
		return nil, fmt.Errorf("unknown format")
	}

	targetLine := 0
	if len(cursorPath) > 0 {
		targetLine = findMatchingLine(lang, newContent, cursorPath, ym)
	}

	return map[string]any{
		"content":    newContent,
		"cursorLine": targetLine,
	}, nil
}

func handleGotoPath(args []any) (any, error) {
	if len(args) < 3 {
		return nil, fmt.Errorf("gotoPath requires [uri, content, pathString]")
	}
	uri, ok := args[0].(string)
	if !ok {
		return nil, fmt.Errorf("first argument must be a URI string")
	}
	content, ok := args[1].(string)
	if !ok {
		return nil, fmt.Errorf("second argument must be content string")
	}
	pathString, ok := args[2].(string)
	if !ok {
		return nil, fmt.Errorf("third argument must be a path string")
	}

	lang := documentLangs[uri]
	if lang == nil {
		return nil, fmt.Errorf("no language found for URI")
	}

	inputTokens := tokenizeInputPath(pathString)
	if len(inputTokens) == 0 {
		return nil, nil
	}

	version := DocumentVersions[uri]
	ym := GetYangModel(lang, version)
	var parsed map[int]ParsedLine
	if sap, ok := lang.(SchemaAwareParser); ok && ym != nil {
		parsed = sap.ParseDocumentWithModel(content, ym)
	} else {
		parsed = lang.ParseDocument(content)
	}

	line, exact := findClosestLine(parsed, inputTokens)
	return map[string]any{
		"line":  line,
		"exact": exact,
	}, nil
}

func tokenizeInputPath(input string) []string {
	s := strings.TrimSpace(input)
	s = strings.TrimPrefix(s, "/")
	s = strings.TrimSpace(s)
	if s == "" {
		return nil
	}
	return TokenizeLine(s)
}

func findClosestLine(parsed map[int]ParsedLine, inputTokens []string) (int, bool) {
	type lineEntry struct {
		num int
		pl  ParsedLine
	}
	entries := make([]lineEntry, 0, len(parsed))
	for num, pl := range parsed {
		entries = append(entries, lineEntry{num, pl})
	}
	sort.Slice(entries, func(i, j int) bool {
		return entries[i].num < entries[j].num
	})

	bestLine := -1
	bestDepth := -1

	for _, e := range entries {
		matchLen := prefixMatchLen(e.pl.PathTokens, inputTokens)
		if matchLen == 0 {
			continue
		}
		if matchLen == len(inputTokens) && matchLen == len(e.pl.PathTokens) {
			return e.num, true
		}
		if matchLen > bestDepth {
			bestDepth = matchLen
			bestLine = e.num
		}
	}
	return bestLine, false
}

func prefixMatchLen(a, b []string) int {
	n := len(a)
	if len(b) < n {
		n = len(b)
	}
	for i := 0; i < n; i++ {
		if !strings.EqualFold(a[i], b[i]) {
			return i
		}
	}
	return n
}

// find the line that matches us
func findMatchingLine(lang Language, content string, cursorPath []string, ym *yang.Model) int {
	var parsed map[int]ParsedLine
	if sap, ok := lang.(SchemaAwareParser); ok && ym != nil {
		parsed = sap.ParseDocumentWithModel(content, ym)
	} else {
		parsed = lang.ParseDocument(content)
	}
	for lineNum, pl := range parsed {
		if slices.Equal(cursorPath, pl.PathTokens) {
			return lineNum
		}
	}
	return 0
}

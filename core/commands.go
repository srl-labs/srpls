package core

import (
	"fmt"

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

// Converter is implemented by languages that support format conversion.
type Converter interface {
	Flatten(content string) string
	Unflatten(content string) string
	UnflattenWithModel(content string, ym *yang.Model) string
}

// WorkspaceExecuteCommand handles workspace/executeCommand requests.
func WorkspaceExecuteCommand(_ *glsp.Context, params *protocol.ExecuteCommandParams) (any, error) {
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

	lang := documentLangs[uri]
	if lang == nil {
		return nil, fmt.Errorf("no language found for URI")
	}

	conv, ok := lang.(Converter)
	if !ok {
		return nil, fmt.Errorf("language does not support conversion")
	}

	format := lang.DetectFormat(content)

	switch format {
	case FormatFlat:
		version := DocumentVersions[uri]
		ym := GetYangModel(lang, version)
		if ym != nil {
			return conv.UnflattenWithModel(content, ym), nil
		}
		return conv.Unflatten(content), nil

	case FormatBrace:
		return conv.Flatten(content), nil
	}

	return nil, fmt.Errorf("unknown format")
}

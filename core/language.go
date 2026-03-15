package core

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/srl-labs/srpls/yang"

	protocol "github.com/tliron/glsp/protocol_3_16"
)

// ParsedLine holds the parsed context for a single line in a config document.
type ParsedLine struct {
	PathTokens  []string
	ParentDepth int
	LeafName    string
	LeafValue   string
	LineText    string
}

// Language defines the contract for a network OS language.
type Language interface {
	Name() string
	SkipDirs() map[string]bool
	RootModules() []string
	ParseDocument(content string) map[int]ParsedLine
	ValidateLine(line ParsedLine, lineNum uint32, ym *yang.Model) []protocol.Diagnostic
}

// ConfigFormat represents the syntax style of a config document.
type ConfigFormat int

const (
	FormatBrace ConfigFormat = iota
	FormatFlat
)

// CompletionContext carries the parsed cursor context for completions.
type CompletionContext struct {
	ParentPath []string
	LineTokens []string
	Prefix     string
	Format     ConfigFormat
	Indent     string
}

type Completer interface {
	ContextAtCursor(content string, line, character uint32) CompletionContext
}

type ValueHinter interface {
	ValueHints(schemaPath string) []string
}

type DocumentValidator interface {
	ValidateDocument(content string) []protocol.Diagnostic
}

type VersionDetector interface {
	DetectVersion(content string) string
}

type SymbolProvider interface {
	DocumentSymbols(content string, ym *yang.Model) []protocol.DocumentSymbol
}

type FoldingProvider interface {
	FoldingRanges(content string) []protocol.FoldingRange
}

type Formatter interface {
	FormatDocument(content string, options protocol.FormattingOptions) []protocol.TextEdit
}

type ModelPathResolver interface {
	YangDirForVersion(version string) string
}

type DefaultVersionProvider interface {
	GetDefaultVersion() string
}

type DefaultLanguage struct {
	LangName          string
	LangSkipDirs      map[string]bool
	LangRootModules   []string
	CommentPrefixes   []string         // ["#"], ["#", "//"]
	SkipBlockPrefixes []string         // ["persistent-indices"] for sros
	VersionPatterns   []*regexp.Regexp // regexes with one capture group for version
	YangDirBase       string           // the subdir UNDER ~/.srpls
	YangDirFilePrefix string
	DefaultVersion    string // fallback version when we can't extract with versionpattern from the doc
	Hints             map[string][]string
}

func (d *DefaultLanguage) Name() string              { return d.LangName }
func (d *DefaultLanguage) SkipDirs() map[string]bool { return d.LangSkipDirs }
func (d *DefaultLanguage) RootModules() []string     { return d.LangRootModules }
func (d *DefaultLanguage) GetDefaultVersion() string { return d.DefaultVersion }

func (d *DefaultLanguage) YangDirForVersion(version string) string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".srpls", d.YangDirBase, "yang", d.YangDirFilePrefix+version)
}

func (d *DefaultLanguage) DetectVersion(content string) string {
	lines := strings.SplitN(content, "\n", 11)
	if len(lines) > 10 {
		lines = lines[:10]
	}
	for _, line := range lines {
		for _, re := range d.VersionPatterns {
			if m := re.FindStringSubmatch(line); m != nil {
				return m[1]
			}
		}
	}
	return ""
}

func (d *DefaultLanguage) ValueHints(schemaPath string) []string {
	if d.Hints == nil {
		return nil
	}
	return d.Hints[schemaPath]
}

func (d *DefaultLanguage) IsComment(trimmed string) bool {
	for _, p := range d.CommentPrefixes {
		if strings.HasPrefix(trimmed, p) {
			return true
		}
	}
	return false
}

func (d *DefaultLanguage) IsSkipBlock(trimmed string) bool {
	for _, prefix := range d.SkipBlockPrefixes {
		if strings.HasPrefix(trimmed, prefix) && strings.Contains(trimmed, "{") {
			return true
		}
	}
	return false
}

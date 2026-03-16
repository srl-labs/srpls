package srlinux

import (
	"fmt"
	"regexp"

	"github.com/srl-labs/srpls/core"
	"github.com/srl-labs/srpls/utils"

	protocol "github.com/tliron/glsp/protocol_3_16"
)

type SRLinux struct {
	core.DefaultLanguage
}

func init() {
	core.Register(&SRLinux{
		DefaultLanguage: core.DefaultLanguage{
			LangName:        "srlinux",
			LangRootModules: []string{"srl_nokia"},
			CommentPrefixes: []string{"#", "//", "!"},
			VersionPatterns: []*regexp.Regexp{
				regexp.MustCompile(`(?:SR Linux|SRL|srl_nokia)[- :v]+(\d+\.\d+)`),
			},
			DefaultVersion:    "25.10",
			YangDirBase:       "srlinux",
			YangDirFilePrefix: "srlinux_",
			Hints: map[string][]string{
				"network-instance": {"default", "mgmt"},
				"subinterface":     {"0"},
			},
		},
	})
}

func (s *SRLinux) DetectPlatform(content string) string {
	return DetectPlatform(content)
}

// ValueHints overrides DefaultLanguage.ValueHints to provide platform-aware
// interface completions based on a "# platform=<name>" comment in the document.
func (s *SRLinux) ValueHints(schemaPath string, content string) []string {
	if schemaPath == "interface" {
		platform := DetectPlatform(content)
		return InterfacesForPlatform(platform)
	}
	return s.DefaultLanguage.ValueHints(schemaPath, content)
}

func (s *SRLinux) HintDetail(schemaPath string, content string) string {
	if schemaPath == "interface" {
		return DetectPlatform(content)
	}
	return ""
}

func (s *SRLinux) ValidateHint(schemaPath string, value string, content string, lineNum, start, end uint32) *protocol.Diagnostic {
	if schemaPath != "interface" {
		return nil
	}

	platform := DetectPlatform(content)
	ifaces := InterfacesForPlatform(platform)

	for _, iface := range ifaces {
		if iface == value {
			return nil
		}
	}

	sev := protocol.DiagnosticSeverityWarning
	src := core.AppName
	return &protocol.Diagnostic{
		Range:    utils.DiagRange(lineNum, start, end),
		Severity: &sev,
		Source:   &src,
		Message:  fmt.Sprintf("Unknown interface '%s' for platform %s", value, platform),
	}
}

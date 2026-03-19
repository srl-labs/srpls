package srlinux

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/srl-labs/srpls/core"
	"github.com/srl-labs/srpls/utils"

	protocol "github.com/tliron/glsp/protocol_3_16"
)

var versionKeyRe = regexp.MustCompile(`(?i)version\s*=\s*(\d+\.\d+)`)

type SRLinux struct {
	core.DefaultLanguage
}

func init() {
	core.Register(&SRLinux{
		DefaultLanguage: core.DefaultLanguage{
			LangName:        "srlinux",
			LangRootModules: []string{"srl_nokia"},
			CommentPrefixes:   []string{"#", "//", "!"},
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

func (s *SRLinux) DetectVersion(content string) string {
	line, _, _ := strings.Cut(content, "\n")
	if m := versionKeyRe.FindStringSubmatch(line); m != nil {
		return m[1]
	}
	return ""
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

func (s *SRLinux) SetVersionDirective(content, version string) string {
	line1, rest, hasRest := strings.Cut(content, "\n")
	if versionKeyRe.MatchString(line1) {
		line1 = versionKeyRe.ReplaceAllString(line1, "version="+version)
	} else if strings.HasPrefix(strings.TrimSpace(line1), "#") || strings.HasPrefix(strings.TrimSpace(line1), "//") || strings.HasPrefix(strings.TrimSpace(line1), "!") {
		line1 = line1 + " version=" + version
	} else {
		line1 = "# version=" + version + "\n" + line1
	}
	if hasRest {
		return line1 + "\n" + rest
	}
	return line1
}

func (s *SRLinux) SetPlatformDirective(content, platform string) string {
	line1, rest, hasRest := strings.Cut(content, "\n")
	if platformRe.MatchString(line1) {
		line1 = platformRe.ReplaceAllString(line1, "platform="+platform)
	} else if strings.HasPrefix(strings.TrimSpace(line1), "#") || strings.HasPrefix(strings.TrimSpace(line1), "//") || strings.HasPrefix(strings.TrimSpace(line1), "!") {
		line1 = line1 + " platform=" + platform
	} else {
		line1 = "# platform=" + platform + "\n" + line1
	}
	if hasRest {
		return line1 + "\n" + rest
	}
	return line1
}

func (s *SRLinux) KnownPlatforms() []string {
	return KnownPlatformNames()
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

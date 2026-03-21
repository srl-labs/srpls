package sros

import (
	"regexp"
	"strings"

	"github.com/srl-labs/srpls/core"
)

var srosVersionKeyRe = regexp.MustCompile(`(?i)version\s*=\s*(\S+)`)
var srosPlatformRe = regexp.MustCompile(`(?i)platform\s*=\s*\S+`)

type SROS struct {
	core.DefaultLanguage
}

// DetectVersion checks line 1 for version= key, then falls back to
// TiMOS / Configuration format version within the first 10 lines.
func (s *SROS) DetectVersion(content string) string {
	line1, _, _ := strings.Cut(content, "\n")
	if m := srosVersionKeyRe.FindStringSubmatch(line1); m != nil {
		return strings.ToUpper(m[1])
	}
	return strings.ToUpper(s.DefaultLanguage.DetectVersion(content))
}

func (s *SROS) SetVersionDirective(content, version string) string {
	line1, rest, hasRest := strings.Cut(content, "\n")
	if srosVersionKeyRe.MatchString(line1) {
		line1 = srosVersionKeyRe.ReplaceAllString(line1, "version="+version)
		if hasRest {
			return line1 + "\n" + rest
		}
		return line1
	}
	return "# version=" + version + "\n" + content
}

func (s *SROS) SetPlatformDirective(content, platform string) string {
	line1, rest, hasRest := strings.Cut(content, "\n")
	if srosPlatformRe.MatchString(line1) {
		line1 = srosPlatformRe.ReplaceAllString(line1, "platform="+platform)
		if hasRest {
			return line1 + "\n" + rest
		}
		return line1
	}
	return "# platform=" + platform + "\n" + content
}

func init() {
	core.Register(&SROS{
		DefaultLanguage: core.DefaultLanguage{
			LangName:          "sros",
			LangRootModules:   []string{"nokia-conf"},
			CommentPrefixes:   []string{"#"},
			SkipBlockPrefixes: []string{"persistent-indices"},
			VersionPatterns: []*regexp.Regexp{
				regexp.MustCompile(`TiMOS-[A-Z]-(\d+\.\d+\.\S+)`),
			},
			FlatPrefix:        "/",
			DefaultVersion:    "",
			YangDirBase:       "sros",
			YangDirFilePrefix: "latest_sros_",
			Hints: map[string][]string{
				"configure/router": {"Base"},
			},
			LangSkipDirs:  map[string]bool{"nokia-submodule": true},
			QuoteListKeys: true,
		},
	})
}

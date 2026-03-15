package srlinux

import (
	"regexp"

	"github.com/srl-labs/srpls/core"
)

type SRLinux struct {
	core.DefaultLanguage
}

func init() {
	core.Register(&SRLinux{
		DefaultLanguage: core.DefaultLanguage{
			LangName:        "srlinux",
			LangRootModules: []string{"srl_nokia"},
			CommentPrefixes: []string{"#", "//"},
			VersionPatterns: []*regexp.Regexp{
				regexp.MustCompile(`(?:SR Linux|SRL|srl_nokia)[- :v]+(\d+\.\d+)`),
			},
			DefaultVersion:    "25.10",
			YangDirBase:       "srlinux",
			YangDirFilePrefix: "srlinux_",
			Hints: map[string][]string{
				"network-instance": {"default", "mgmt"},
			},
		},
	})
}

package sros

import (
	"regexp"

	"github.com/srl-labs/srpls/core"
)

type SROS struct {
	core.DefaultLanguage
}

func init() {
	core.Register(&SROS{
		DefaultLanguage: core.DefaultLanguage{
			LangName:          "sros",
			LangRootModules:   []string{"nokia-conf"},
			CommentPrefixes:   []string{"#"},
			SkipBlockPrefixes: []string{"persistent-indices"},
			VersionPatterns: []*regexp.Regexp{
				regexp.MustCompile(`TiMOS-[A-Z]-(\d+\.\d+)`),
				regexp.MustCompile(`Configuration format version (\d+\.\d+)`),
			},
			DefaultVersion:    "25.10",
			YangDirBase:       "sros",
			YangDirFilePrefix: "latest_sros_",
			Hints: map[string][]string{
				"configure/router": {"Base"},
			},
			LangSkipDirs: map[string]bool{"nokia-submodule": true},
		},
	})
}

package core

import "github.com/srl-labs/srpls/yang"

var (
	Registry   []Language
	YangModels = make(map[string]*yang.Model)
)

func Register(l Language) {
	Registry = append(Registry, l)
}

func GetLanguage(uri string) Language {
	for _, l := range Registry {
		if l.MatchesURI(uri) {
			return l
		}
	}
	return nil
}

func GetYangModel(l Language, version string) *yang.Model {
	if version != "" {
		return YangModels[l.Name()+":"+version]
	}
	return YangModels[l.Name()]
}

func SetYangModel(langName, version string, ym *yang.Model) {
	if version == "" {
		YangModels[langName] = ym
	} else {
		YangModels[langName+":"+version] = ym
	}
}

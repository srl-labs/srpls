package core

import "github.com/srl-labs/srpls/yang"

var (
	Registry   []Language
	YangModels = make(map[string]*yang.Model)
)

func Register(l Language) {
	// Set Owner so DefaultLanguage methods can access the outer type's interfaces.
	type ownable interface{ SetOwner(Language) }
	if o, ok := l.(ownable); ok {
		o.SetOwner(l)
	}
	Registry = append(Registry, l)
}

func FilterRegistry(name string) {
	for _, l := range Registry {
		if l.Name() == name {
			Registry = []Language{l}
			return
		}
	}
}

func GetLanguage(name string) Language {
	for _, l := range Registry {
		if l.Name() == name {
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

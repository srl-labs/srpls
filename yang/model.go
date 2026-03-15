package yang

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	goyang "github.com/openconfig/goyang/pkg/yang"
	"github.com/tliron/commonlog"
)

type Model struct {
	Root map[string]*goyang.Entry
}

func Load(modelsDir string, skipDirs map[string]bool, rootModules []string) (*Model, error) {
	log := commonlog.GetLogger("")

	yangFiles, dirs, err := collectYangFiles(modelsDir, skipDirs)
	if err != nil {
		return nil, fmt.Errorf("walking models dir: %w", err)
	}
	log.Infof("found %d .yang files in %d directories", len(yangFiles), len(dirs))

	ms := parseModules(log, yangFiles, dirs)
	processModules(log, ms)

	root := buildRoot(ms, rootModules)
	log.Infof("schema loaded: %d top-level nodes", len(root))
	return &Model{Root: root}, nil
}

func collectYangFiles(modelsDir string, skipDirs map[string]bool) ([]string, map[string]bool, error) {
	dirs := make(map[string]bool)
	var yangFiles []string

	err := filepath.WalkDir(modelsDir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() && skipDirs[d.Name()] {
			return filepath.SkipDir
		}
		if !d.IsDir() && strings.HasSuffix(d.Name(), ".yang") {
			yangFiles = append(yangFiles, path)
			dirs[filepath.Dir(path)] = true
		}
		return nil
	})
	return yangFiles, dirs, err
}

func parseModules(log commonlog.Logger, yangFiles []string, dirs map[string]bool) *goyang.Modules {
	ms := goyang.NewModules()
	for dir := range dirs {
		ms.AddPath(dir)
	}
	for _, f := range yangFiles {
		data, err := os.ReadFile(f)
		if err != nil {
			log.Warningf("reading %s: %v", f, err)
			continue
		}
		if err := ms.Parse(string(data), f); err != nil {
			log.Warningf("parsing %s: %v", f, err)
		}
	}
	return ms
}

func processModules(log commonlog.Logger, ms *goyang.Modules) {
	defer func() {
		if r := recover(); r != nil {
			log.Warningf("goyang panicked during Process(): %v", r)
		}
	}()
	errs := ms.Process()
	for _, err := range errs {
		log.Warningf("process: %v", err)
	}
}

func buildRoot(ms *goyang.Modules, rootModules []string) map[string]*goyang.Entry {
	root := make(map[string]*goyang.Entry)
	for _, mod := range ms.Modules {
		if mod == nil || !matchesRootModules(mod.Name, rootModules) {
			continue
		}
		entry := goyang.ToEntry(mod)
		if entry == nil || entry.Dir == nil {
			continue
		}
		for name, child := range entry.Dir {
			if child == nil {
				continue
			}
			mergeEntry(root, name, child)
		}
	}
	return root
}

func mergeEntry(root map[string]*goyang.Entry, name string, child *goyang.Entry) {
	existing, ok := root[name]
	if !ok {
		root[name] = child
		return
	}
	if len(child.Dir) > len(existing.Dir) {
		for k, v := range existing.Dir {
			if _, has := child.Dir[k]; !has {
				child.Dir[k] = v
			}
		}
		root[name] = child
	} else if child.Dir != nil {
		for k, v := range child.Dir {
			if _, has := existing.Dir[k]; !has {
				existing.Dir[k] = v
			}
		}
	}
}

func matchesRootModules(modName string, prefixes []string) bool {
	if len(prefixes) == 0 {
		return true
	}
	for _, p := range prefixes {
		if strings.HasPrefix(modName, p) {
			return true
		}
	}
	return false
}

func FlattenChoices(entries map[string]*goyang.Entry) map[string]*goyang.Entry {
	result := make(map[string]*goyang.Entry)
	for name, entry := range entries {
		if entry.ReadOnly() {
			// ignore state yang entries (config false;)
			continue
		}
		if entry.Kind == goyang.ChoiceEntry || entry.Kind == goyang.CaseEntry {
			for k, v := range FlattenChoices(entry.Dir) {
				result[k] = v
			}
		} else {
			result[name] = entry
		}
	}
	return result
}

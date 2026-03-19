package srlinux

import (
	"embed"
	"encoding/json"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

//go:embed interfaces/*.json
var interfaceFS embed.FS

type platformData struct {
	Platform   string   `json:"platform"`
	ShortName  string   `json:"shortname"`
	Interfaces []string `json:"interfaces"`
}

// platformInterfaces maps full platform name (e.g. "7220-IXR-D2L") to interface names.
var platformInterfaces map[string][]string

// platformAliases maps shortnames and full names (lowercased) to the canonical platform name.
var platformAliases map[string]string

const defaultPlatform = "7220-IXR-D2L"

var platformRe = regexp.MustCompile(`(?i)platform\s*=\s*(\S+)`)

func init() {
	platformInterfaces = make(map[string][]string)
	platformAliases = make(map[string]string)

	entries, err := interfaceFS.ReadDir("interfaces")
	if err != nil {
		return
	}

	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
			continue
		}

		data, err := interfaceFS.ReadFile(filepath.Join("interfaces", e.Name()))
		if err != nil {
			continue
		}

		var pd platformData
		if json.Unmarshal(data, &pd) != nil || pd.Platform == "" {
			continue
		}

		platformInterfaces[pd.Platform] = pd.Interfaces

		// Register aliases: full name and shortname (both lowercased)
		platformAliases[strings.ToLower(pd.Platform)] = pd.Platform
		if pd.ShortName != "" {
			platformAliases[strings.ToLower(pd.ShortName)] = pd.Platform
		}
	}
}

// DetectPlatform extracts the platform from a "# platform=<name>" comment
// in the first few lines of a config file. Returns the full platform name.
func DetectPlatform(content string) string {
	lines := strings.SplitN(content, "\n", 6)
	if len(lines) > 5 {
		lines = lines[:5]
	}

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if !strings.HasPrefix(trimmed, "#") && !strings.HasPrefix(trimmed, "//") && !strings.HasPrefix(trimmed, "!") {
			continue
		}
		if m := platformRe.FindStringSubmatch(trimmed); m != nil {
			return resolvePlatform(m[1])
		}
	}
	return defaultPlatform
}

func resolvePlatform(name string) string {
	if full, ok := platformAliases[strings.ToLower(name)]; ok {
		return full
	}
	return defaultPlatform
}

// KnownPlatformNames returns the sorted list of all known platform names.
func KnownPlatformNames() []string {
	names := make([]string, 0, len(platformInterfaces))
	for name := range platformInterfaces {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// InterfacesForPlatform returns the interface names for a given platform,
// falling back to the default platform.
func InterfacesForPlatform(platform string) []string {
	if ifaces, ok := platformInterfaces[platform]; ok {
		return ifaces
	}
	return platformInterfaces[defaultPlatform]
}

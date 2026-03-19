package srlinux

import (
	"embed"
	"encoding/base64"
	"fmt"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

//go:embed chassis/*.svg
var chassisFS embed.FS

// chassisSVGs maps lower-cased platform name to embedded SVG content.
var chassisSVGs map[string]string

func init() {
	chassisSVGs = make(map[string]string)
	entries, err := chassisFS.ReadDir("chassis")
	if err != nil {
		return
	}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".svg") {
			continue
		}
		data, err := chassisFS.ReadFile(filepath.Join("chassis", e.Name()))
		if err != nil {
			continue
		}
		platform := strings.ToLower(filenameToPlatform(strings.TrimSuffix(e.Name(), ".svg")))
		chassisSVGs[platform] = string(data)
	}
}

func filenameToPlatform(name string) string {
	name = strings.TrimSpace(name)
	if name == "" {
		return ""
	}
	replacer := strings.NewReplacer(" ", "-", "_", "-")
	name = replacer.Replace(name)
	for strings.Contains(name, "--") {
		name = strings.ReplaceAll(name, "--", "-")
	}
	return strings.Trim(name, "-")
}

// RenderFrontPanel generates a markdown image of the platform front panel
// with the given interface highlighted.
func (s *SRLinux) RenderFrontPanel(interfaceName string, content string) string {
	if !strings.HasPrefix(interfaceName, "ethernet-") && !strings.HasPrefix(interfaceName, "mgmt") {
		return ""
	}

	platform := DetectPlatform(content)

	svg, ok := chassisSVGs[strings.ToLower(platform)]
	if !ok {
		return ""
	}

	// Determine the path ID to highlight
	var portID string
	if strings.HasPrefix(interfaceName, "mgmt") {
		portID = interfaceName // "mgmt0"
	} else {
		portID = strconv.Itoa(parsePortNumber(interfaceName)) // "1", "49", etc.
	}

	if portID == "" || portID == "0" {
		return ""
	}

	highlighted := highlightPortByID(svg, portID)
	b64 := base64.StdEncoding.EncodeToString([]byte(highlighted))
	return fmt.Sprintf("![%s](data:image/svg+xml;base64,%s)", platform, b64)
}

var (
	styleAttrRE = regexp.MustCompile(`\bstyle="([^"]*)"`)
	fillStyleRE = regexp.MustCompile(`(?i)(fill\s*:\s*)([^;"]+)`)
	fillAttrRE  = regexp.MustCompile(`\bfill="[^"]*"`)
)

// highlightPortByID finds the SVG element with the given id and changes its fill.
func highlightPortByID(svg string, portID string) string {
	pattern := fmt.Sprintf(`<([a-zA-Z][a-zA-Z0-9:_-]*)\b[^>]*\bid="%s"[^>]*>`, regexp.QuoteMeta(portID))
	re := regexp.MustCompile(pattern)
	return re.ReplaceAllStringFunc(svg, highlightElement)
}

func highlightElement(tag string) string {
	const highlightColor = "#005aff"

	styleLoc := styleAttrRE.FindStringSubmatchIndex(tag)
	if styleLoc != nil {
		styleValue := tag[styleLoc[2]:styleLoc[3]]
		if fillStyleRE.MatchString(styleValue) {
			styleValue = fillStyleRE.ReplaceAllString(styleValue, "${1}"+highlightColor)
		} else {
			trimmed := strings.TrimSpace(styleValue)
			if trimmed != "" && !strings.HasSuffix(trimmed, ";") {
				styleValue += ";"
			}
			styleValue += "fill:" + highlightColor
		}
		return tag[:styleLoc[2]] + styleValue + tag[styleLoc[3]:]
	}

	if fillAttrRE.MatchString(tag) {
		return fillAttrRE.ReplaceAllString(tag, `fill="`+highlightColor+`"`)
	}

	insertAt := strings.LastIndex(tag, "/>")
	if insertAt == -1 {
		insertAt = strings.LastIndex(tag, ">")
	}
	if insertAt == -1 {
		return tag
	}
	return tag[:insertAt] + ` fill="` + highlightColor + `"` + tag[insertAt:]
}

func parsePortNumber(iface string) int {
	idx := strings.LastIndex(iface, "/")
	if idx < 0 {
		return 0
	}
	n, err := strconv.Atoi(iface[idx+1:])
	if err != nil {
		return 0
	}
	return n
}

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

// chassisSVGs maps platform name to embedded SVG content.
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
		platform := filenameToPlatform(strings.TrimSuffix(e.Name(), ".svg"))
		chassisSVGs[platform] = string(data)
	}
}

func filenameToPlatform(name string) string {
	return strings.Replace(name, "_", "-", 1)
}

// RenderFrontPanel generates a markdown image of the platform front panel
// with the given interface highlighted.
func (s *SRLinux) RenderFrontPanel(interfaceName string, content string) string {
	if !strings.HasPrefix(interfaceName, "ethernet-") && !strings.HasPrefix(interfaceName, "mgmt") {
		return ""
	}

	platform := DetectPlatform(content)

	svg, ok := chassisSVGs[platform]
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

// highlightPortByID finds the path with the given id and changes its fill.
func highlightPortByID(svg string, portID string) string {
	// Match: id="portID" ... style="...fill:XXXX..."
	// We need to find the path with this exact id and change its fill color.
	pattern := fmt.Sprintf(`(<path\s[^>]*id="%s"[^>]*style="[^"]*)(fill:[^;]+)`, regexp.QuoteMeta(portID))
	re := regexp.MustCompile(pattern)

	result := re.ReplaceAllString(svg, `${1}fill:#2979FF`)

	// Also try: style before id
	pattern2 := fmt.Sprintf(`(<path\s[^>]*style="[^"]*)(fill:[^;]+)([^"]*"[^>]*id="%s")`, regexp.QuoteMeta(portID))
	re2 := regexp.MustCompile(pattern2)

	result = re2.ReplaceAllString(result, `${1}fill:#2979FF${3}`)

	return result
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

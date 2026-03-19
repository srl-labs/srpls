package srlinux

import (
	"embed"
	"encoding/base64"
	"fmt"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/beevik/etree"
)

const (
	highlightColor = "#005aff"
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
		platform := strings.ToLower(strings.TrimSuffix(e.Name(), ".svg"))
		chassisSVGs[platform] = string(data)
	}
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

// highlightPortByID finds the SVG element with the given id and changes its fill.
func highlightPortByID(svg string, portID string) string {
	doc := etree.NewDocument()
	if err := doc.ReadFromString(svg); err != nil {
		return svg
	}

	el := doc.FindElement(fmt.Sprintf("//*[@id='%s']", portID))
	if el == nil {
		return svg
	}

	el.RemoveAttr("style")
	el.CreateAttr("fill", highlightColor)

	out, err := doc.WriteToString()
	if err != nil {
		return svg
	}
	return out
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

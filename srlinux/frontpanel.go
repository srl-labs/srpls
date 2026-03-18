package srlinux

import (
	"embed"
	"encoding/base64"
	"fmt"
	"io/fs"
	"math"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

//go:embed chassis/*.svg chassis/*/*.svg
var chassisFS embed.FS

// chassisSVGs maps lower-cased platform name to embedded SVG content.
var chassisSVGs map[string]string

func init() {
	chassisSVGs = make(map[string]string)
	if err := fs.WalkDir(chassisFS, "chassis", func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.EqualFold(filepath.Ext(d.Name()), ".svg") {
			return nil
		}
		data, readErr := chassisFS.ReadFile(path)
		if readErr != nil {
			return nil
		}
		platform := strings.ToLower(filenameToPlatform(strings.TrimSuffix(d.Name(), filepath.Ext(d.Name()))))
		if platform == "" {
			return nil
		}
		chassisSVGs[platform] = string(data)
		return nil
	}); err != nil {
		return
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
	highlighted = normalizeSVGDisplaySize(highlighted)
	b64 := base64.StdEncoding.EncodeToString([]byte(highlighted))
	return fmt.Sprintf("![%s](data:image/svg+xml;base64,%s)", platform, b64)
}

var (
	styleAttrRE      = regexp.MustCompile(`\bstyle="([^"]*)"`)
	fillStyleRE      = regexp.MustCompile(`(?i)(fill\s*:\s*)([^;"]+)`)
	fillAttrRE       = regexp.MustCompile(`\bfill="[^"]*"`)
	svgTagRE         = regexp.MustCompile(`(?is)<svg\b[^>]*>`)
	svgViewBoxRE     = regexp.MustCompile(`(?i)\bviewBox="([^"]+)"`)
	svgWidthAttrRE   = regexp.MustCompile(`(?i)\bwidth="[^"]*"`)
	svgHeightAttrRE  = regexp.MustCompile(`(?i)\bheight="[^"]*"`)
	svgTrailing0sRE  = regexp.MustCompile(`(\.\d*?[1-9])0+$`)
	svgTrailingDotRE = regexp.MustCompile(`\.0+$`)
)

const hoverSVGWidth = 457.0

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

func normalizeSVGDisplaySize(svg string) string {
	loc := svgTagRE.FindStringIndex(svg)
	if loc == nil {
		return svg
	}

	svgTag := svg[loc[0]:loc[1]]
	m := svgViewBoxRE.FindStringSubmatch(svgTag)
	if len(m) != 2 {
		return svg
	}

	viewBoxFields := strings.Fields(m[1])
	if len(viewBoxFields) != 4 {
		return svg
	}

	viewBoxWidth, err := strconv.ParseFloat(viewBoxFields[2], 64)
	if err != nil || viewBoxWidth <= 0 {
		return svg
	}
	viewBoxHeight, err := strconv.ParseFloat(viewBoxFields[3], 64)
	if err != nil || viewBoxHeight <= 0 {
		return svg
	}

	targetHeight := hoverSVGWidth * viewBoxHeight / viewBoxWidth
	svgTag = upsertSVGAttr(svgTag, "width", formatSVGSize(hoverSVGWidth))
	svgTag = upsertSVGAttr(svgTag, "height", formatSVGSize(targetHeight))

	return svg[:loc[0]] + svgTag + svg[loc[1]:]
}

func upsertSVGAttr(tag string, attr string, value string) string {
	re := svgWidthAttrRE
	switch attr {
	case "height":
		re = svgHeightAttrRE
	}

	if re.MatchString(tag) {
		return re.ReplaceAllString(tag, fmt.Sprintf(`%s="%s"`, attr, value))
	}

	insertAt := strings.LastIndex(tag, ">")
	if insertAt == -1 {
		return tag
	}
	return tag[:insertAt] + fmt.Sprintf(` %s="%s"`, attr, value) + tag[insertAt:]
}

func formatSVGSize(v float64) string {
	rounded := math.Round(v)
	if math.Abs(v-rounded) < 0.0005 {
		return strconv.FormatInt(int64(rounded), 10)
	}

	s := strconv.FormatFloat(v, 'f', 3, 64)
	s = svgTrailing0sRE.ReplaceAllString(s, "$1")
	s = svgTrailingDotRE.ReplaceAllString(s, "")
	return s
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

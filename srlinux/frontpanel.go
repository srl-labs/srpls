package srlinux

import (
	"bytes"
	"embed"
	"encoding/base64"
	"fmt"
	"image"
	"image/jpeg"
	"image/png"
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
		chassisSVGs[platform] = optimizeSVGForHover(string(data))
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
	return fmt.Sprintf("![%s](%s)", platform, svgDataURI(highlighted))
}

var (
	styleAttrRE       = regexp.MustCompile(`\bstyle="([^"]*)"`)
	fillStyleRE       = regexp.MustCompile(`(?i)(fill\s*:\s*)([^;"]+)`)
	fillAttrRE        = regexp.MustCompile(`\bfill="[^"]*"`)
	svgTagRE          = regexp.MustCompile(`(?is)<svg\b[^>]*>`)
	svgViewBoxRE      = regexp.MustCompile(`(?i)\bviewBox="([^"]+)"`)
	svgWidthAttrRE    = regexp.MustCompile(`(?i)\bwidth="[^"]*"`)
	svgHeightAttrRE   = regexp.MustCompile(`(?i)\bheight="[^"]*"`)
	svgXMLDeclRE      = regexp.MustCompile(`(?is)^\s*<\?xml[^>]*\?>\s*`)
	svgXMLNSXLinkRE   = regexp.MustCompile(`\s+xmlns:xlink="[^"]*"`)
	svgXLinkHrefRE    = regexp.MustCompile(`\s+xlink:href="[^"]*"`)
	svgImagePNGHrefRE = regexp.MustCompile(`(?i)(\bhref="data:image/png;base64,)([^"]+)(")`)
	svgTrailing0sRE   = regexp.MustCompile(`(\.\d*?[1-9])0+$`)
	svgTrailingDotRE  = regexp.MustCompile(`\.0+$`)
	svgDataURIRepl    = strings.NewReplacer(
		"%", "%25",
		"#", "%23",
		"<", "%3C",
		">", "%3E",
		" ", "%20",
		"(", "%28",
		")", "%29",
		"{", "%7B",
		"}", "%7D",
	)
)

const (
	hoverSVGWidth          = 457.0
	hoverMarkdownTargetLen = 100000
	hoverRasterMaxWidth    = 640
	hoverJPEGQuality       = 60
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

func optimizeSVGForHover(svg string) string {
	svg = stripLegacyXLinkAttrs(svg)
	if estimatedHoverMarkdownLen(svg) <= hoverMarkdownTargetLen {
		return svg
	}

	compacted := compactEmbeddedPNG(svg, hoverRasterMaxWidth, hoverJPEGQuality)
	if compacted == "" {
		return svg
	}
	if estimatedHoverMarkdownLen(compacted) >= estimatedHoverMarkdownLen(svg) {
		return svg
	}
	return compacted
}

func stripLegacyXLinkAttrs(svg string) string {
	svg = svgXMLNSXLinkRE.ReplaceAllString(svg, "")
	svg = svgXLinkHrefRE.ReplaceAllString(svg, "")
	return svg
}

func estimatedHoverMarkdownLen(svg string) int {
	return len("![](") + len(svgDataURI(svg)) + len(")")
}

func compactEmbeddedPNG(svg string, maxWidth, quality int) string {
	if maxWidth <= 0 {
		return svg
	}
	if quality < 1 {
		quality = 1
	}
	if quality > 100 {
		quality = 100
	}

	m := svgImagePNGHrefRE.FindStringSubmatchIndex(svg)
	if m == nil {
		return svg
	}

	raw, err := base64.StdEncoding.DecodeString(svg[m[4]:m[5]])
	if err != nil {
		return svg
	}
	img, err := png.Decode(bytes.NewReader(raw))
	if err != nil {
		return svg
	}

	bounds := img.Bounds()
	origW := bounds.Dx()
	origH := bounds.Dy()
	if origW <= 0 || origH <= 0 {
		return svg
	}

	targetW := origW
	if targetW > maxWidth {
		targetW = maxWidth
	}
	targetH := int(math.Round(float64(origH) * float64(targetW) / float64(origW)))
	if targetH < 1 {
		targetH = 1
	}

	out := image.Image(img)
	if targetW != origW || targetH != origH {
		out = resizeNearest(out, targetW, targetH)
	}

	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, out, &jpeg.Options{Quality: quality}); err != nil {
		return svg
	}
	encoded := base64.StdEncoding.EncodeToString(buf.Bytes())

	replacement := fmt.Sprintf(`href="data:image/jpeg;base64,%s"`, encoded)
	return svg[:m[2]] + replacement + svg[m[7]:]
}

func resizeNearest(src image.Image, targetW, targetH int) *image.RGBA {
	dst := image.NewRGBA(image.Rect(0, 0, targetW, targetH))
	srcBounds := src.Bounds()
	srcW := srcBounds.Dx()
	srcH := srcBounds.Dy()
	if srcW <= 0 || srcH <= 0 {
		return dst
	}

	for y := 0; y < targetH; y++ {
		srcY := srcBounds.Min.Y + y*srcH/targetH
		for x := 0; x < targetW; x++ {
			srcX := srcBounds.Min.X + x*srcW/targetW
			dst.Set(x, y, src.At(srcX, srcY))
		}
	}
	return dst
}

func svgDataURI(svg string) string {
	svg = svgXMLDeclRE.ReplaceAllString(svg, "")
	svg = strings.TrimSpace(svg)
	svg = strings.ReplaceAll(svg, "\r", "")
	svg = strings.ReplaceAll(svg, "\n", "")
	svg = strings.ReplaceAll(svg, "\t", " ")
	svg = strings.ReplaceAll(svg, `"`, `'`)
	return "data:image/svg+xml;utf8," + svgDataURIRepl.Replace(svg)
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

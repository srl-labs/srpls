package core

import (
	"fmt"
	"strings"

	"github.com/srl-labs/srpls/utils"
	"github.com/srl-labs/srpls/yang"

	goyang "github.com/openconfig/goyang/pkg/yang"
	protocol "github.com/tliron/glsp/protocol_3_16"
)

func TokenizeLine(s string) []string {
	var tokens []string
	rem := strings.TrimSpace(s)
	for rem != "" {
		if rem[0] == '"' {
			end := strings.Index(rem[1:], "\"")
			if end >= 0 {
				tokens = append(tokens, rem[1:end+1])
				rem = strings.TrimSpace(rem[end+2:])
			} else {
				tokens = append(tokens, rem[1:])
				rem = ""
			}
		} else {
			end := strings.IndexByte(rem, ' ')
			if end >= 0 {
				tokens = append(tokens, rem[:end])
				rem = strings.TrimSpace(rem[end+1:])
			} else {
				tokens = append(tokens, rem)
				rem = ""
			}
		}
	}
	return tokens
}

func (d *DefaultLanguage) DetectFormat(content string) ConfigFormat {
	for _, line := range strings.Split(content, "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || d.IsComment(trimmed) {
			continue
		}
		if strings.HasPrefix(trimmed, "/") || strings.HasPrefix(trimmed, "set /") {
			return FormatFlat
		}
		return FormatBrace
	}
	return FormatFlat
}

func (d *DefaultLanguage) ParseBraceDocument(content string) map[int]ParsedLine {
	lines := strings.Split(content, "\n")
	result := make(map[int]ParsedLine)

	var stack []string
	var depthStack []int
	skipDepth := 0

	for lineNum := 0; lineNum < len(lines); lineNum++ {
		line := lines[lineNum]
		trimmed := strings.TrimSpace(line)

		if trimmed == "" || d.IsComment(trimmed) {
			continue
		}

		if skipDepth > 0 {
			skipDepth += strings.Count(trimmed, "{") - strings.Count(trimmed, "}")
			continue
		}
		if d.IsSkipBlock(trimmed) {
			skipDepth = 1
			continue
		}

		if trimmed == "exit" || trimmed == "exit all" {
			continue
		}

		// Leaf-list values enclosed in [ ... ] — parse the keyword, skip values
		if strings.HasSuffix(trimmed, "[") {
			leafName := strings.TrimSpace(strings.TrimSuffix(trimmed, "["))
			if leafName != "" {
				pathTokens := make([]string, 0, len(stack)+1)
				pathTokens = append(pathTokens, stack...)
				pathTokens = append(pathTokens, leafName)
				result[lineNum] = ParsedLine{
					PathTokens:  pathTokens,
					ParentDepth: len(stack),
					LeafName:    leafName,
					LineText:    line,
				}
			}
			for lineNum++; lineNum < len(lines); lineNum++ {
				if strings.TrimSpace(lines[lineNum]) == "]" {
					break
				}
			}
			continue
		}

		// Multi-line strings — parse the keyword, skip string content
		if strings.Count(trimmed, "\"")%2 != 0 {
			tokens := TokenizeLine(trimmed)
			if len(tokens) >= 1 {
				leafName := tokens[0]
				pathTokens := make([]string, 0, len(stack)+1)
				pathTokens = append(pathTokens, stack...)
				pathTokens = append(pathTokens, leafName)
				result[lineNum] = ParsedLine{
					PathTokens:  pathTokens,
					ParentDepth: len(stack),
					LeafName:    leafName,
					LineText:    line,
				}
			}
			for lineNum++; lineNum < len(lines); lineNum++ {
				if strings.Contains(lines[lineNum], "\"") {
					break
				}
			}
			continue
		}

		if trimmed == "}" {
			if len(depthStack) > 0 {
				stack = stack[:depthStack[len(depthStack)-1]]
				depthStack = depthStack[:len(depthStack)-1]
			}
			continue
		}

		if strings.HasSuffix(trimmed, "{") {
			blockPart := strings.TrimSpace(strings.TrimSuffix(trimmed, "{"))
			tokens := TokenizeLine(blockPart)
			if len(tokens) >= 1 {
				parentDepth := len(stack)
				depthStack = append(depthStack, parentDepth)
				stack = append(stack, tokens...)

				pathTokens := make([]string, len(stack))
				copy(pathTokens, stack)
				result[lineNum] = ParsedLine{
					PathTokens:  pathTokens,
					ParentDepth: parentDepth,
					LineText:    line,
				}
			}
			continue
		}

		if strings.Contains(trimmed, "}") {
			for range strings.Count(trimmed, "}") {
				if len(depthStack) > 0 {
					stack = stack[:depthStack[len(depthStack)-1]]
					depthStack = depthStack[:len(depthStack)-1]
				}
			}
			continue
		}

		tokens := TokenizeLine(trimmed)
		if len(tokens) >= 1 {
			leafName := tokens[0]
			leafValue := ""
			if len(tokens) >= 2 {
				afterKeyword := strings.TrimSpace(trimmed[len(leafName):])
				leafValue = utils.StripQuotes(afterKeyword)
			}

			pathTokens := make([]string, 0, len(stack)+1)
			pathTokens = append(pathTokens, stack...)
			pathTokens = append(pathTokens, leafName)

			result[lineNum] = ParsedLine{
				PathTokens:  pathTokens,
				ParentDepth: len(stack),
				LeafName:    leafName,
				LeafValue:   leafValue,
				LineText:    line,
			}
		}
	}
	return result
}

func (d *DefaultLanguage) ParseFlatDocument(content string) map[int]ParsedLine {
	lines := strings.Split(content, "\n")
	result := make(map[int]ParsedLine)

	for lineNum := 0; lineNum < len(lines); lineNum++ {
		line := lines[lineNum]
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || d.IsComment(trimmed) {
			continue
		}

		var body string
		switch {
		case strings.HasPrefix(trimmed, "set / "):
			body = trimmed[6:]
		case strings.HasPrefix(trimmed, "/"):
			body = trimmed[1:]
		default:
			continue
		}

		// Skip multi-line strings (unclosed quote on this line)
		if strings.Count(body, "\"")%2 != 0 {
			for lineNum++; lineNum < len(lines); lineNum++ {
				if strings.Contains(lines[lineNum], "\"") {
					break
				}
			}
		}

		// Strip inline leaf-list values: "keyword [ val1 val2 ]" → "keyword"
		if bracketIdx := strings.Index(body, " ["); bracketIdx >= 0 {
			body = strings.TrimSpace(body[:bracketIdx])
		}

		// Strip quoted value from body for tokenization
		if quoteIdx := strings.Index(body, "\""); quoteIdx >= 0 {
			body = strings.TrimSpace(body[:quoteIdx])
		}

		body = strings.TrimSpace(body)
		tokens := TokenizeLine(body)
		if len(tokens) == 0 {
			continue
		}

		result[lineNum] = ParsedLine{
			PathTokens:  tokens,
			ParentDepth: 0,
			LineText:    line,
		}
	}
	return result
}

func (d *DefaultLanguage) ParseDocument(content string) map[int]ParsedLine {
	if d.DetectFormat(content) == FormatFlat {
		return d.ParseFlatDocument(content)
	}
	return d.ParseBraceDocument(content)
}

// ParseDocumentWithModel parses the document using the YANG model for
// schema-aware tokenization of flat-format lines.
func (d *DefaultLanguage) ParseDocumentWithModel(content string, ym *yang.Model) map[int]ParsedLine {
	if d.DetectFormat(content) == FormatFlat {
		return d.ParseFlatDocumentWithModel(content, ym)
	}
	return d.ParseBraceDocument(content)
}

// ParseFlatDocumentWithModel parses flat-format config using the YANG model
// to correctly identify path tokens, leaf names, and leaf values.
func (d *DefaultLanguage) ParseFlatDocumentWithModel(content string, ym *yang.Model) map[int]ParsedLine {
	if ym == nil {
		return d.ParseFlatDocument(content)
	}

	lines := strings.Split(content, "\n")
	result := make(map[int]ParsedLine)

	for lineNum := 0; lineNum < len(lines); lineNum++ {
		line := lines[lineNum]
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || d.IsComment(trimmed) {
			continue
		}

		var body string
		switch {
		case strings.HasPrefix(trimmed, "set / "):
			body = trimmed[6:]
		case strings.HasPrefix(trimmed, "/"):
			body = trimmed[1:]
		default:
			continue
		}

		startLineNum := lineNum

		// Handle multi-line strings (unclosed quote on this line)
		if strings.Count(body, "\"")%2 != 0 {
			for lineNum++; lineNum < len(lines); lineNum++ {
				body += "\n" + lines[lineNum]
				if strings.Contains(lines[lineNum], "\"") {
					break
				}
			}
		}

		pl := parseFlatLineWithModel(body, line, ym)
		result[startLineNum] = pl
	}
	return result
}

// parseFlatLineWithModel walks the YANG tree to classify tokens in a flat line.
func parseFlatLineWithModel(body string, lineText string, ym *yang.Model) ParsedLine {
	children := yang.FlattenChoices(ym.Root)
	var pathTokens []string
	var leafName, leafValue string

	rest := strings.TrimSpace(body)

	for rest != "" {
		tok, tokEnd := nextToken(rest)
		if tok == "" {
			break
		}
		rest = strings.TrimSpace(rest[tokEnd:])

		// Leaf-list bracket — collect values
		if tok == "[" {
			var vals []string
			for rest != "" {
				v, vEnd := nextToken(rest)
				rest = strings.TrimSpace(rest[vEnd:])
				if v == "]" {
					break
				}
				vals = append(vals, v)
			}
			leafValue = "[ " + strings.Join(vals, " ") + " ]"
			break
		}

		entry, ok := children[tok]
		if !ok {
			// Unknown token — treat remaining as opaque value
			pathTokens = append(pathTokens, tok)
			if rest != "" {
				leafValue = rest
				rest = ""
			}
			break
		}

		pathTokens = append(pathTokens, tok)

		switch {
		case entry.Kind == goyang.DirectoryEntry && entry.IsList():
			// Consume key value tokens
			keyCount := listKeyTokenCount(entry)
			for k := 0; k < keyCount && rest != ""; k++ {
				keyTok, keyEnd := nextToken(rest)
				if keyTok == "" {
					break
				}
				pathTokens = append(pathTokens, keyTok)
				rest = strings.TrimSpace(rest[keyEnd:])
			}
			if entry.Dir != nil {
				children = yang.FlattenChoices(entry.Dir)
			}

		case entry.Kind == goyang.DirectoryEntry:
			if entry.Dir != nil {
				children = yang.FlattenChoices(entry.Dir)
			}

		case entry.Kind == goyang.LeafEntry:
			leafName = tok
			leafValue = utils.StripQuotes(strings.TrimSpace(rest))
			rest = ""
		}
	}

	return ParsedLine{
		PathTokens:  pathTokens,
		ParentDepth: 0,
		LeafName:    leafName,
		LeafValue:   leafValue,
		LineText:    lineText,
	}
}

// nextToken extracts the next whitespace-delimited or quoted token from s.
// Returns the token value and the byte offset past the token in s.
func nextToken(s string) (string, int) {
	if len(s) == 0 {
		return "", 0
	}
	if s[0] == '"' {
		end := strings.Index(s[1:], "\"")
		if end >= 0 {
			return s[1 : end+1], end + 2
		}
		return s[1:], len(s)
	}
	end := strings.IndexAny(s, " \t")
	if end < 0 {
		return s, len(s)
	}
	return s[:end], end
}

func (d *DefaultLanguage) ContextAtCursor(content string, line, character uint32) CompletionContext {
	if d.DetectFormat(content) == FormatFlat {
		return d.FlatContextAtCursor(content, line, character)
	}
	return d.BraceContextAtCursor(content, line, character)
}

func (d *DefaultLanguage) BraceContextAtCursor(content string, line, character uint32) CompletionContext {
	lines := strings.Split(content, "\n")

	var stack []string
	var depthStack []int
	skipDepth := 0

	for i := 0; i < int(line) && i < len(lines); i++ {
		trimmed := strings.TrimSpace(lines[i])

		if trimmed == "" || d.IsComment(trimmed) {
			continue
		}

		if skipDepth > 0 {
			skipDepth += strings.Count(trimmed, "{") - strings.Count(trimmed, "}")
			continue
		}
		if d.IsSkipBlock(trimmed) {
			skipDepth = 1
			continue
		}

		if trimmed == "exit" || trimmed == "exit all" {
			continue
		}

		// Skip leaf-list values enclosed in [ ... ]
		if strings.HasSuffix(trimmed, "[") {
			for i++; i < int(line) && i < len(lines); i++ {
				if strings.TrimSpace(lines[i]) == "]" {
					break
				}
			}
			continue
		}

		// Skip multi-line strings (opening " without closing " on the same line)
		if strings.Count(trimmed, "\"")%2 != 0 {
			for i++; i < int(line) && i < len(lines); i++ {
				if strings.Contains(lines[i], "\"") {
					break
				}
			}
			continue
		}

		if trimmed == "}" {
			if len(depthStack) > 0 {
				stack = stack[:depthStack[len(depthStack)-1]]
				depthStack = depthStack[:len(depthStack)-1]
			}
			continue
		}

		if strings.HasSuffix(trimmed, "{") {
			blockPart := strings.TrimSpace(strings.TrimSuffix(trimmed, "{"))
			tokens := TokenizeLine(blockPart)
			if len(tokens) >= 1 {
				depthStack = append(depthStack, len(stack))
				stack = append(stack, tokens...)
			}
			continue
		}

		if strings.Contains(trimmed, "}") {
			for range strings.Count(trimmed, "}") {
				if len(depthStack) > 0 {
					stack = stack[:depthStack[len(depthStack)-1]]
					depthStack = depthStack[:len(depthStack)-1]
				}
			}
			continue
		}
	}

	if int(line) < len(lines) {
		cursorTrimmed := strings.TrimSpace(lines[line])
		if cursorTrimmed == "}" {
			if len(depthStack) > 0 {
				stack = stack[:depthStack[len(depthStack)-1]]
			}
		}
	}

	parentPath := make([]string, len(stack))
	copy(parentPath, stack)

	var lineText string
	var indent string
	if int(line) < len(lines) {
		fullLine := lines[line]
		indent = fullLine[:len(fullLine)-len(strings.TrimLeft(fullLine, " \t"))]
		pos := int(character)
		if pos > len(fullLine) {
			pos = len(fullLine)
		}
		lineText = fullLine[:pos]
	}

	lineText = strings.TrimLeft(lineText, " \t")

	if d.IsComment(lineText) {
		return CompletionContext{ParentPath: parentPath, Format: FormatBrace, Indent: indent}
	}

	tokens := TokenizeLine(lineText)

	var lineTokens []string
	var prefix string

	if len(tokens) == 0 {
		// Empty line.
	} else if lineText == "" || lineText[len(lineText)-1] == ' ' || lineText[len(lineText)-1] == '\t' {
		lineTokens = tokens
	} else {
		lineTokens = tokens[:len(tokens)-1]
		prefix = tokens[len(tokens)-1]
	}

	return CompletionContext{
		ParentPath: parentPath,
		LineTokens: lineTokens,
		Prefix:     prefix,
		Format:     FormatBrace,
		Indent:     indent,
	}
}

func (d *DefaultLanguage) FlatContextAtCursor(content string, line, character uint32) CompletionContext {
	lines := strings.Split(content, "\n")

	var lineText string
	if int(line) < len(lines) {
		fullLine := lines[line]
		pos := int(character)
		if pos > len(fullLine) {
			pos = len(fullLine)
		}
		lineText = fullLine[:pos]
	}

	lineText = strings.TrimLeft(lineText, " \t")

	if d.IsComment(lineText) {
		return CompletionContext{Format: FormatFlat}
	}

	hasLeader := strings.HasPrefix(lineText, "/") || strings.HasPrefix(lineText, "set /")
	if strings.HasPrefix(lineText, "set / ") {
		lineText = lineText[6:]
	} else {
		lineText = strings.TrimPrefix(lineText, "/")
	}

	// Strip inline leaf-list content
	if bracketIdx := strings.Index(lineText, " ["); bracketIdx >= 0 {
		lineText = strings.TrimSpace(lineText[:bracketIdx])
	}

	tokens := TokenizeLine(lineText)

	var lineTokens []string
	var prefix string

	if len(tokens) == 0 {
		// Just "/" or empty.
	} else if lineText == "" || lineText[len(lineText)-1] == ' ' || lineText[len(lineText)-1] == '\t' {
		lineTokens = tokens
	} else {
		lineTokens = tokens[:len(tokens)-1]
		prefix = tokens[len(tokens)-1]
	}

	return CompletionContext{
		ParentPath: nil,
		LineTokens: lineTokens,
		Prefix:     prefix,
		Format:     FormatFlat,
		HasLeader:  hasLeader,
	}
}

func (d *DefaultLanguage) ValidateLine(pl ParsedLine, lineNum uint32, ym *yang.Model, content string) []protocol.Diagnostic {
	sev := protocol.DiagnosticSeverityError
	src := AppName
	tokens := pl.PathTokens
	if len(tokens) == 0 {
		return nil
	}

	line := pl.LineText
	current := yang.FlattenChoices(ym.Root)
	i := 0

	for i < len(tokens) {
		token := tokens[i]
		entry, ok := current[token]
		if !ok {
			if i < pl.ParentDepth {
				return nil
			}
			start, end := utils.SafePosition(line, token)
			return []protocol.Diagnostic{{
				Range: utils.DiagRange(lineNum, start, end), Severity: &sev, Source: &src,
				Message: fmt.Sprintf("Unknown path element '%s'", token),
			}}
		}
		i++

		switch {
		case entry.Kind == goyang.DirectoryEntry && entry.IsList():
			if i >= len(tokens) {
				break
			}

			// validate all key values for compound keys
			keys := strings.Fields(entry.Key)
			if i-1 >= pl.ParentDepth {
				keyIdx := i // first key value token
				for k, keyName := range keys {
					valIdx := keyIdx
					if k > 0 {
						valIdx++ // skip the key keyword token
					}
					if valIdx >= len(tokens) {
						break
					}
					keyVal := tokens[valIdx]
					keyStart, keyEnd := utils.SafePosition(line, keyVal)

					// validate against YANG type
					if entry.Dir != nil {
						if keyLeaf, ok := entry.Dir[keyName]; ok && keyLeaf != nil && keyLeaf.Type != nil {
							if diag := ValidateValueAgainstType(keyLeaf.Type, keyVal, lineNum, keyStart, keyEnd); diag != nil {
								return []protocol.Diagnostic{*diag}
							}
						}
					}

					// validate against hints (first key only)
					if k == 0 && d.Owner != nil {
						if hv, ok := d.Owner.(HintValidator); ok {
							sp := schemaPath(entry)
							if diag := hv.ValidateHint(sp, keyVal, content, lineNum, keyStart, keyEnd); diag != nil {
								return []protocol.Diagnostic{*diag}
							}
						}
					}

					if k == 0 {
						keyIdx++
					} else {
						keyIdx += 2
					}
				}
			}

			// skip key tokens: first key is 1 token (value only),
			// each additional key is 2 tokens (keyword + value)
			i += listKeyTokenCount(entry)
			if entry.Dir != nil {
				current = yang.FlattenChoices(entry.Dir)
			} else {
				return nil
			}

		case entry.Kind == goyang.DirectoryEntry:
			if entry.Dir != nil {
				current = yang.FlattenChoices(entry.Dir)
			} else {
				return nil
			}

		case entry.Kind == goyang.LeafEntry:
			value := pl.LeafValue
			if value == "" && i < len(tokens) {
				value = tokens[i]
			}
			// Skip validation for leaf-list bracket values
			if value != "" && !strings.HasPrefix(value, "[") {
				valStart, valEnd := utils.SafePosition(line, value)
				if diag := ValidateLeafValue(entry, value, lineNum, valStart, valEnd); diag != nil {
					return []protocol.Diagnostic{*diag}
				}
			}
			return nil
		}
	}
	return nil
}

func (d *DefaultLanguage) ValidateDocument(content string) []protocol.Diagnostic {
	format := d.DetectFormat(content)
	diags := d.validateMixedFormat(content, format)
	if format == FormatBrace {
		diags = append(diags, d.validateBraces(content)...)
	}
	return diags
}

func (d *DefaultLanguage) validateMixedFormat(content string, format ConfigFormat) []protocol.Diagnostic {
	sev := protocol.DiagnosticSeverityError
	src := AppName
	lines := strings.Split(content, "\n")
	var diags []protocol.Diagnostic

	for lineNum, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || d.IsComment(trimmed) {
			continue
		}

		switch format {
		case FormatFlat:
			if strings.Contains(trimmed, "{") || trimmed == "}" {
				diags = append(diags, protocol.Diagnostic{
					Range: utils.DiagRange(uint32(lineNum), 0, uint32(len(line))), Severity: &sev, Source: &src,
					Message: "Brace-style syntax not allowed in flat-format file",
				})
			}
		case FormatBrace:
			if strings.HasPrefix(trimmed, "/") {
				diags = append(diags, protocol.Diagnostic{
					Range: utils.DiagRange(uint32(lineNum), 0, uint32(len(line))), Severity: &sev, Source: &src,
					Message: "Flat-style path not allowed in brace-format file",
				})
			}
		}
	}
	return diags
}

type braceInfo struct {
	line   uint32
	col    uint32
	indent int
	label  string
}

func (d *DefaultLanguage) validateBraces(content string) []protocol.Diagnostic {
	sev := protocol.DiagnosticSeverityError
	src := AppName
	lines := strings.Split(content, "\n")
	var stack []braceInfo
	var diags []protocol.Diagnostic
	skipDepth := 0

	for lineNum, line := range lines {
		trimmed := strings.TrimSpace(line)

		if trimmed == "" || d.IsComment(trimmed) {
			continue
		}

		if skipDepth > 0 {
			skipDepth += strings.Count(trimmed, "{") - strings.Count(trimmed, "}")
			continue
		}
		if d.IsSkipBlock(trimmed) {
			skipDepth = 1
			continue
		}

		indent := utils.LineIndent(line)

		for i, ch := range line {
			switch ch {
			case '{':
				label := strings.TrimSpace(strings.TrimSuffix(trimmed, "{"))
				stack = append(stack, braceInfo{
					line:   uint32(lineNum),
					col:    uint32(i),
					indent: indent,
					label:  label,
				})
			case '}':
				if len(stack) == 0 {
					candidate := findMissingOpen(lines, lineNum, indent, d)
					msg := "Unmatched closing brace '}'"
					if candidate != "" {
						msg = fmt.Sprintf("Unmatched '}' — did '%s' lose its opening '{'?", candidate)
					}
					diags = append(diags, protocol.Diagnostic{
						Range: utils.DiagRange(uint32(lineNum), uint32(i), uint32(i+1)), Severity: &sev, Source: &src,
						Message: msg,
					})
				} else {
					top := stack[len(stack)-1]
					if top.indent == indent {
						stack = stack[:len(stack)-1]
					} else {
						matchIdx := -1
						for j := len(stack) - 1; j >= 0; j-- {
							if stack[j].indent == indent {
								matchIdx = j
								break
							}
						}
						if matchIdx >= 0 {
							for j := len(stack) - 1; j > matchIdx; j-- {
								diags = append(diags, protocol.Diagnostic{
									Range: utils.DiagRange(stack[j].line, stack[j].col, stack[j].col+1), Severity: &sev, Source: &src,
									Message: fmt.Sprintf("Missing closing '}' for '%s'", stack[j].label),
								})
							}
							stack = stack[:matchIdx]
						} else {
							candidate := findMissingOpen(lines, lineNum, indent, d)
							msg := "Unmatched closing brace '}'"
							if candidate != "" {
								msg = fmt.Sprintf("Unmatched '}' — did '%s' lose its opening '{'?", candidate)
							}
							diags = append(diags, protocol.Diagnostic{
								Range: utils.DiagRange(uint32(lineNum), uint32(i), uint32(i+1)), Severity: &sev, Source: &src,
								Message: msg,
							})
						}
					}
				}
			}
		}
	}

	for _, b := range stack {
		diags = append(diags, protocol.Diagnostic{
			Range: utils.DiagRange(b.line, b.col, b.col+1), Severity: &sev, Source: &src,
			Message: fmt.Sprintf("Missing closing '}' for '%s'", b.label),
		})
	}

	return diags
}

func findMissingOpen(lines []string, closeLine int, indent int, d *DefaultLanguage) string {
	for i := closeLine - 1; i >= 0; i-- {
		l := lines[i]
		trimmed := strings.TrimSpace(l)
		if trimmed == "" || trimmed == "}" || d.IsComment(trimmed) {
			continue
		}
		if strings.Contains(trimmed, "{") || strings.Contains(trimmed, "}") {
			continue
		}
		if utils.LineIndent(l) == indent {
			return trimmed
		}
	}
	return ""
}

type blockRange struct {
	name      string
	startLine int
	endLine   int
	children  []*blockRange
	leaves    []leafEntry
}

type leafEntry struct {
	name, value string
	line        int
}

func (d *DefaultLanguage) buildBlockTree(content string) []*blockRange {
	lines := strings.Split(content, "\n")
	var roots []*blockRange
	var stack []*blockRange
	skipDepth := 0

	for lineNum := 0; lineNum < len(lines); lineNum++ {
		line := lines[lineNum]
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || d.IsComment(trimmed) {
			continue
		}

		if skipDepth > 0 {
			skipDepth += strings.Count(trimmed, "{") - strings.Count(trimmed, "}")
			continue
		}
		if d.IsSkipBlock(trimmed) {
			skipDepth = 1
			continue
		}

		if trimmed == "exit" || trimmed == "exit all" {
			continue
		}

		// Skip leaf-list values enclosed in [ ... ]
		if strings.HasSuffix(trimmed, "[") {
			for lineNum++; lineNum < len(lines); lineNum++ {
				if strings.TrimSpace(lines[lineNum]) == "]" {
					break
				}
			}
			continue
		}

		// Skip multi-line strings (opening " without closing " on the same line)
		if strings.Count(trimmed, "\"")%2 != 0 {
			for lineNum++; lineNum < len(lines); lineNum++ {
				if strings.Contains(lines[lineNum], "\"") {
					break
				}
			}
			continue
		}

		if trimmed == "}" {
			if len(stack) > 0 {
				stack[len(stack)-1].endLine = lineNum
				stack = stack[:len(stack)-1]
			}
			continue
		}

		if strings.HasSuffix(trimmed, "{") {
			blockName := strings.TrimSpace(strings.TrimSuffix(trimmed, "{"))
			if blockName == "" {
				blockName = "{}"
			}
			br := &blockRange{
				name:      blockName,
				startLine: lineNum,
				endLine:   lineNum,
			}
			if len(stack) > 0 {
				stack[len(stack)-1].children = append(stack[len(stack)-1].children, br)
			} else {
				roots = append(roots, br)
			}
			stack = append(stack, br)
			continue
		}

		if strings.Contains(trimmed, "}") {
			for range strings.Count(trimmed, "}") {
				if len(stack) > 0 {
					stack[len(stack)-1].endLine = lineNum
					stack = stack[:len(stack)-1]
				}
			}
			continue
		}

		tokens := TokenizeLine(trimmed)
		if len(tokens) >= 1 && tokens[0] != "" {
			le := leafEntry{
				name: tokens[0],
				line: lineNum,
			}
			if len(tokens) >= 2 {
				le.value = strings.TrimSpace(trimmed[len(tokens[0]):])
			}
			if len(stack) > 0 {
				stack[len(stack)-1].leaves = append(stack[len(stack)-1].leaves, le)
			}
		}
	}

	return roots
}

func (d *DefaultLanguage) DocumentSymbols(content string, ym *yang.Model) []protocol.DocumentSymbol {
	if d.DetectFormat(content) == FormatFlat {
		return d.flatDocumentSymbols(content)
	}
	return d.braceDocumentSymbols(content)
}

func (d *DefaultLanguage) braceDocumentSymbols(content string) []protocol.DocumentSymbol {
	tree := d.buildBlockTree(content)
	var symbols []protocol.DocumentSymbol
	for _, br := range tree {
		symbols = append(symbols, blockToSymbol(br))
	}
	return symbols
}

func blockToSymbol(br *blockRange) protocol.DocumentSymbol {
	kind := protocol.SymbolKindStruct
	rng := protocol.Range{
		Start: protocol.Position{Line: uint32(br.startLine), Character: 0},
		End:   protocol.Position{Line: uint32(br.endLine), Character: 0},
	}
	selRng := protocol.Range{
		Start: protocol.Position{Line: uint32(br.startLine), Character: 0},
		End:   protocol.Position{Line: uint32(br.startLine), Character: uint32(len(br.name))},
	}
	sym := protocol.DocumentSymbol{
		Name:           br.name,
		Kind:           kind,
		Range:          rng,
		SelectionRange: selRng,
	}
	for _, child := range br.children {
		sym.Children = append(sym.Children, blockToSymbol(child))
	}
	for _, leaf := range br.leaves {
		leafKind := protocol.SymbolKindProperty
		leafRng := protocol.Range{
			Start: protocol.Position{Line: uint32(leaf.line), Character: 0},
			End:   protocol.Position{Line: uint32(leaf.line), Character: uint32(len(leaf.name))},
		}
		detail := leaf.value
		sym.Children = append(sym.Children, protocol.DocumentSymbol{
			Name:           leaf.name,
			Detail:         &detail,
			Kind:           leafKind,
			Range:          leafRng,
			SelectionRange: leafRng,
		})
	}
	return sym
}

func (d *DefaultLanguage) flatDocumentSymbols(content string) []protocol.DocumentSymbol {
	lines := strings.Split(content, "\n")
	var symbols []protocol.DocumentSymbol
	for lineNum, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || d.IsComment(trimmed) || (!strings.HasPrefix(trimmed, "/") && !strings.HasPrefix(trimmed, "set /")) {
			continue
		}
		kind := protocol.SymbolKindProperty
		rng := protocol.Range{
			Start: protocol.Position{Line: uint32(lineNum), Character: 0},
			End:   protocol.Position{Line: uint32(lineNum), Character: uint32(len(line))},
		}
		symbols = append(symbols, protocol.DocumentSymbol{
			Name:           trimmed,
			Kind:           kind,
			Range:          rng,
			SelectionRange: rng,
		})
	}
	return symbols
}

func (d *DefaultLanguage) FoldingRanges(content string) []protocol.FoldingRange {
	if d.DetectFormat(content) == FormatFlat {
		return nil
	}
	tree := d.buildBlockTree(content)
	var ranges []protocol.FoldingRange
	collectFoldingRanges(tree, &ranges)
	ranges = append(ranges, d.commentFoldingRanges(content)...)
	return ranges
}

func collectFoldingRanges(blocks []*blockRange, ranges *[]protocol.FoldingRange) {
	for _, br := range blocks {
		if br.endLine > br.startLine {
			startLine := uint32(br.startLine)
			endLine := uint32(br.endLine)
			*ranges = append(*ranges, protocol.FoldingRange{
				StartLine: startLine,
				EndLine:   endLine,
			})
		}
		collectFoldingRanges(br.children, ranges)
	}
}

func (d *DefaultLanguage) commentFoldingRanges(content string) []protocol.FoldingRange {
	lines := strings.Split(content, "\n")
	var ranges []protocol.FoldingRange
	commentKind := string(protocol.FoldingRangeKindComment)

	inComment := false
	commentStart := 0

	for lineNum, line := range lines {
		trimmed := strings.TrimSpace(line)
		isCommentLine := d.IsComment(trimmed)

		if isCommentLine && !inComment {
			inComment = true
			commentStart = lineNum
		} else if !isCommentLine && inComment {
			inComment = false
			if lineNum-1 > commentStart {
				startLine := uint32(commentStart)
				endLine := uint32(lineNum - 1)
				ranges = append(ranges, protocol.FoldingRange{
					StartLine: startLine,
					EndLine:   endLine,
					Kind:      &commentKind,
				})
			}
		}
	}

	if inComment && len(lines)-1 > commentStart {
		startLine := uint32(commentStart)
		endLine := uint32(len(lines) - 1)
		ranges = append(ranges, protocol.FoldingRange{
			StartLine: startLine,
			EndLine:   endLine,
			Kind:      &commentKind,
		})
	}

	return ranges
}

func (d *DefaultLanguage) FormatDocument(content string, options protocol.FormattingOptions) []protocol.TextEdit {
	if d.DetectFormat(content) == FormatFlat {
		return flatFormat(content)
	}
	return d.braceFormat(content, options)
}

func (d *DefaultLanguage) braceFormat(content string, options protocol.FormattingOptions) []protocol.TextEdit {
	tabSize := 4
	useSpaces := true

	if ts, ok := options["tabSize"]; ok {
		if v, ok := ts.(float64); ok {
			tabSize = int(v)
		}
	}
	if is, ok := options["insertSpaces"]; ok {
		if v, ok := is.(bool); ok {
			useSpaces = v
		}
	}

	indentUnit := "\t"
	if useSpaces {
		indentUnit = strings.Repeat(" ", tabSize)
	}

	lines := strings.Split(content, "\n")
	var result []string
	depth := 0
	skipDepth := 0

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)

		if trimmed == "" {
			result = append(result, "")
			continue
		}

		if skipDepth > 0 {
			result = append(result, line)
			skipDepth += strings.Count(trimmed, "{") - strings.Count(trimmed, "}")
			continue
		}
		if d.IsSkipBlock(trimmed) {
			result = append(result, strings.Repeat(indentUnit, depth)+trimmed)
			skipDepth = 1
			continue
		}

		if trimmed == "}" {
			if depth > 0 {
				depth--
			}
			result = append(result, strings.Repeat(indentUnit, depth)+trimmed)
			continue
		}

		if strings.Contains(trimmed, "}") && !strings.Contains(trimmed, "{") {
			closes := strings.Count(trimmed, "}")
			for i := 0; i < closes; i++ {
				if depth > 0 {
					depth--
				}
			}
			result = append(result, strings.Repeat(indentUnit, depth)+trimmed)
			continue
		}

		result = append(result, strings.Repeat(indentUnit, depth)+trimmed)

		if strings.HasSuffix(trimmed, "{") {
			depth++
		}
	}

	for i, line := range result {
		result[i] = strings.TrimRight(line, " \t")
	}

	formatted := strings.Join(result, "\n")
	lastLine := uint32(len(lines) - 1)
	lastChar := uint32(len(lines[len(lines)-1]))

	return []protocol.TextEdit{{
		Range: protocol.Range{
			Start: protocol.Position{Line: 0, Character: 0},
			End:   protocol.Position{Line: lastLine, Character: lastChar},
		},
		NewText: formatted,
	}}
}

func flatFormat(content string) []protocol.TextEdit {
	lines := strings.Split(content, "\n")
	var result []string
	changed := false

	for _, line := range lines {
		trimmed := strings.TrimRight(line, " \t")
		if trimmed != line {
			changed = true
		}
		result = append(result, trimmed)
	}

	if !changed {
		return nil
	}

	formatted := strings.Join(result, "\n")
	lastLine := uint32(len(lines) - 1)
	lastChar := uint32(len(lines[len(lines)-1]))

	return []protocol.TextEdit{{
		Range: protocol.Range{
			Start: protocol.Position{Line: 0, Character: 0},
			End:   protocol.Position{Line: lastLine, Character: lastChar},
		},
		NewText: formatted,
	}}
}

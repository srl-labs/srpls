package core

import (
	"sort"
	"strings"

	"github.com/srl-labs/srpls/yang"

	goyang "github.com/openconfig/goyang/pkg/yang"
)

// Flatten converts brace-format config to flat format.
func (d *DefaultLanguage) Flatten(content string) string {
	preamble, body := splitLeadingPreamble(content, d.IsComment)
	lines := strings.Split(strings.TrimRight(body, "\n"), "\n")
	flatPrefix := d.FlatLinePrefix()
	var out []string
	var stack []string
	var depthStack []int
	// Track whether each block depth emitted any content (leaves/children).
	// If a block closes without content, emit an empty container line.
	var hasContent []bool
	skipDepth := 0

	for i := 0; i < len(lines); i++ {
		line := lines[i]
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

		if trimmed == "}" {
			// Check if the closing block had any content
			if len(hasContent) > 0 && !hasContent[len(hasContent)-1] {
				// Empty block — emit the container/list path
				path := flatPath(stack)
				if path != "" {
					out = append(out, flatPrefix+strings.TrimSpace(path))
				}
			}
			if len(depthStack) > 0 {
				stack = stack[:depthStack[len(depthStack)-1]]
				depthStack = depthStack[:len(depthStack)-1]
			}
			if len(hasContent) > 0 {
				hasContent = hasContent[:len(hasContent)-1]
			}
			continue
		}

		// Mark parent block as having content
		if len(hasContent) > 0 {
			hasContent[len(hasContent)-1] = true
		}

		// Leaf-list: keyword [ val1 val2 ... ]
		if strings.HasSuffix(trimmed, "[") {
			leafName := strings.TrimSpace(strings.TrimSuffix(trimmed, "["))
			var vals []string
			for i++; i < len(lines); i++ {
				inner := strings.TrimSpace(lines[i])
				if inner == "]" {
					break
				}
				if inner != "" {
					vals = append(vals, inner)
				}
			}
			if leafName != "" && len(vals) > 0 {
				path := flatPath(stack)
				out = append(out, flatPrefix+path+leafName+" [ "+strings.Join(vals, " ")+" ]")
			}
			continue
		}

		// Multi-line string: collect all lines into one value
		if strings.Count(trimmed, "\"")%2 != 0 {
			fullLine := trimmed
			for i++; i < len(lines); i++ {
				fullLine += "\n" + lines[i]
				if strings.Contains(lines[i], "\"") {
					break
				}
			}
			tokens := TokenizeLine(fullLine)
			if len(tokens) >= 2 {
				path := flatPath(stack)
				leafName := tokens[0]
				afterKey := strings.TrimSpace(fullLine[len(leafName):])
				out = append(out, flatPrefix+path+leafName+" "+afterKey)
			}
			continue
		}

		// Handle single-line inline blocks (e.g.: member "x" { }).
		// These are net-zero for scope depth.
		if strings.Contains(trimmed, "{") && strings.Contains(trimmed, "}") &&
			strings.Count(trimmed, "{") == strings.Count(trimmed, "}") {
			blockPart := strings.TrimSpace(trimmed[:strings.Index(trimmed, "{")])
			if blockPart != "" {
				path := flatPath(stack)
				out = append(out, flatPrefix+path+blockPart)
			}
			continue
		}

		if strings.HasSuffix(trimmed, "{") {
			blockPart := strings.TrimSpace(strings.TrimSuffix(trimmed, "{"))
			tokens := TokenizeLine(blockPart)
			if len(tokens) >= 1 {
				depthStack = append(depthStack, len(stack))
				stack = append(stack, tokens...)
				hasContent = append(hasContent, false)
			}
			continue
		}

		if strings.Contains(trimmed, "}") {
			for range strings.Count(trimmed, "}") {
				if len(depthStack) > 0 {
					stack = stack[:depthStack[len(depthStack)-1]]
					depthStack = depthStack[:len(depthStack)-1]
				}
				if len(hasContent) > 0 {
					hasContent = hasContent[:len(hasContent)-1]
				}
			}
			continue
		}

		// Leaf assignment
		path := flatPath(stack)
		out = append(out, flatPrefix+path+trimmed)
	}

	converted := strings.Join(out, "\n")
	if converted != "" {
		converted += "\n"
	}
	return preamble + converted
}

// flatPath returns the path body prefix from a stack of path tokens.
func flatPath(stack []string) string {
	if len(stack) == 0 {
		return ""
	}
	return strings.Join(stack, " ") + " "
}

func splitLeadingPreamble(content string, isComment func(string) bool) (string, string) {
	if content == "" {
		return "", ""
	}

	lines := strings.SplitAfter(content, "\n")
	idx := 0
	for idx < len(lines) {
		raw := strings.TrimRight(lines[idx], "\r\n")
		trimmed := strings.TrimSpace(raw)
		if trimmed == "" || isComment(trimmed) {
			idx++
			continue
		}
		break
	}

	return strings.Join(lines[:idx], ""), strings.Join(lines[idx:], "")
}

// UnflattenWithModel converts flat-format config to brace-format using the
// YANG model to correctly identify where path elements end and leaves begin.
func (d *DefaultLanguage) UnflattenWithModel(content string, ym *yang.Model) string {
	if ym == nil {
		return d.Unflatten(content)
	}

	preamble, body := splitLeadingPreamble(content, d.IsComment)
	lines := strings.Split(strings.TrimRight(body, "\n"), "\n")

	type item struct {
		containerPath []string
		leafPart      string
	}

	var items []item

	for i := 0; i < len(lines); i++ {
		trimmed := strings.TrimSpace(lines[i])
		if trimmed == "" || d.IsComment(trimmed) {
			continue
		}

		var body string
		switch {
		case strings.HasPrefix(trimmed, "set / "):
			body = trimmed[6:]
		case strings.HasPrefix(trimmed, "/"):
			body = strings.TrimSpace(trimmed[1:])
		default:
			continue
		}

		// Handle multi-line strings
		if strings.Count(body, "\"")%2 != 0 {
			for i++; i < len(lines); i++ {
				body += "\n" + lines[i]
				if strings.Contains(lines[i], "\"") {
					break
				}
			}
		}

		cp, lp := splitFlatLineWithModel(body, ym)
		items = append(items, item{containerPath: cp, leafPart: lp})
	}

	type node struct {
		name     string
		children []*node
		childMap map[string]*node
		leaves   []string
		order    int
	}

	root := &node{childMap: make(map[string]*node)}
	orderCounter := 0

	for _, it := range items {
		cur := root
		for _, tok := range it.containerPath {
			child, ok := cur.childMap[tok]
			if !ok {
				child = &node{name: tok, childMap: make(map[string]*node), order: orderCounter}
				orderCounter++
				cur.childMap[tok] = child
				cur.children = append(cur.children, child)
			}
			cur = child
		}
		if it.leafPart != "" {
			cur.leaves = append(cur.leaves, it.leafPart)
		}
	}

	var sb strings.Builder

	var writeNode func(n *node, indent string)
	writeNode = func(n *node, indent string) {
		sort.Slice(n.children, func(i, j int) bool {
			return n.children[i].order < n.children[j].order
		})

		for _, child := range n.children {
			if len(child.children) == 0 && len(child.leaves) == 0 {
				sb.WriteString(indent + child.name + " {\n")
				sb.WriteString(indent + "}\n")
			} else {
				sb.WriteString(indent + child.name + " {\n")
				for _, leaf := range child.leaves {
					if strings.Contains(leaf, "[") && strings.Contains(leaf, "]") {
						bracketIdx := strings.Index(leaf, "[")
						keyword := strings.TrimSpace(leaf[:bracketIdx])
						listContent := strings.TrimSpace(leaf[bracketIdx+1 : len(leaf)-1])
						vals := strings.Fields(listContent)
						sb.WriteString(indent + "    " + keyword + " [\n")
						for _, v := range vals {
							sb.WriteString(indent + "        " + v + "\n")
						}
						sb.WriteString(indent + "    ]\n")
					} else {
						sb.WriteString(indent + "    " + leaf + "\n")
					}
				}
				writeNode(child, indent+"    ")
				sb.WriteString(indent + "}\n")
			}
		}
	}

	writeNode(root, "")

	return preamble + sb.String()
}

// splitFlatLineWithModel uses the YANG model to split a flat line body into
// container path tokens and a leaf part (leaf name + value).
func splitFlatLineWithModel(body string, ym *yang.Model) (containerPath []string, leafPart string) {
	children := yang.FlattenChoices(ym.Root)
	rest := strings.TrimSpace(body)

	for rest != "" {
		tok, tokEnd := nextToken(rest)
		if tok == "" {
			break
		}
		afterTok := strings.TrimSpace(rest[tokEnd:])

		entry, ok := children[tok]
		if !ok {
			// Unknown token — treat as value for previous container
			leafPart = rest
			return
		}

		switch {
		case entry.Kind == goyang.DirectoryEntry && entry.IsList():
			// List: token + key values form a single container node
			var listParts []string
			listParts = append(listParts, tok)
			rest = afterTok
			keyCount := listKeyTokenCount(entry)
			for k := 0; k < keyCount && rest != ""; k++ {
				keyTok, keyEnd := nextToken(rest)
				if keyTok == "" {
					break
				}
				listParts = append(listParts, keyTok)
				rest = strings.TrimSpace(rest[keyEnd:])
			}
			// Join list name + keys as a single container node name
			containerPath = append(containerPath, strings.Join(listParts, " "))
			if entry.Dir != nil {
				children = yang.FlattenChoices(entry.Dir)
			}

		case entry.Kind == goyang.DirectoryEntry:
			containerPath = append(containerPath, tok)
			rest = afterTok
			if entry.Dir != nil {
				children = yang.FlattenChoices(entry.Dir)
			}

		case entry.Kind == goyang.LeafEntry:
			// Leaf: name + remaining value
			val := strings.TrimSpace(afterTok)
			if val != "" {
				leafPart = tok + " " + strings.TrimSpace(afterTok)
			} else {
				leafPart = tok
			}
			return
		}
	}
	return
}

// Unflatten converts flat-format config to brace-format config.
// This is the heuristic version without YANG model access.
func (d *DefaultLanguage) Unflatten(content string) string {
	preamble, body := splitLeadingPreamble(content, d.IsComment)
	lines := strings.Split(strings.TrimRight(body, "\n"), "\n")

	type entry struct {
		path  []string
		value string // leaf value, or "[...]" for leaf-list
	}

	var entries []entry

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || d.IsComment(trimmed) {
			continue
		}

		var body string
		switch {
		case strings.HasPrefix(trimmed, "set / "):
			body = trimmed[6:]
		case strings.HasPrefix(trimmed, "/"):
			body = strings.TrimSpace(trimmed[1:])
		default:
			continue
		}

		// Handle leaf-list: ... keyword [val1 val2]
		if bracketIdx := strings.Index(body, "["); bracketIdx >= 0 {
			pathPart := strings.TrimSpace(body[:bracketIdx])
			listPart := body[bracketIdx:]
			tokens := TokenizeLine(pathPart)
			entries = append(entries, entry{path: tokens, value: listPart})
			continue
		}

		tokens := TokenizeLine(body)
		if len(tokens) == 0 {
			continue
		}

		entries = append(entries, entry{path: tokens})
	}

	type node struct {
		name     string
		children []*node
		childMap map[string]*node
		leaves   []string
		order    int
	}

	root := &node{childMap: make(map[string]*node)}
	orderCounter := 0

	for _, e := range entries {
		cur := root
		pathTokens := e.path
		if len(pathTokens) == 0 {
			continue
		}

		var containerPath []string
		var leafPart string

		if e.value != "" {
			containerPath = pathTokens[:len(pathTokens)-1]
			leafPart = pathTokens[len(pathTokens)-1] + " " + e.value
		} else if len(pathTokens) >= 2 {
			containerPath = pathTokens[:len(pathTokens)-2]
			leafPart = strings.Join(pathTokens[len(pathTokens)-2:], " ")
		} else {
			containerPath = pathTokens[:len(pathTokens)-1]
			leafPart = pathTokens[len(pathTokens)-1]
		}

		for _, tok := range containerPath {
			child, ok := cur.childMap[tok]
			if !ok {
				child = &node{name: tok, childMap: make(map[string]*node), order: orderCounter}
				orderCounter++
				cur.childMap[tok] = child
				cur.children = append(cur.children, child)
			}
			cur = child
		}

		cur.leaves = append(cur.leaves, leafPart)
	}

	var sb strings.Builder

	var writeNode func(n *node, indent string)
	writeNode = func(n *node, indent string) {
		sort.Slice(n.children, func(i, j int) bool {
			return n.children[i].order < n.children[j].order
		})

		for _, child := range n.children {
			if len(child.children) == 0 && len(child.leaves) == 0 {
				sb.WriteString(indent + child.name + " {\n")
				sb.WriteString(indent + "}\n")
			} else {
				sb.WriteString(indent + child.name + " {\n")
				for _, leaf := range child.leaves {
					if strings.Contains(leaf, "[") && strings.Contains(leaf, "]") {
						bracketIdx := strings.Index(leaf, "[")
						keyword := strings.TrimSpace(leaf[:bracketIdx])
						listContent := strings.TrimSpace(leaf[bracketIdx+1 : len(leaf)-1])
						vals := strings.Fields(listContent)
						sb.WriteString(indent + "    " + keyword + " [\n")
						for _, v := range vals {
							sb.WriteString(indent + "        " + v + "\n")
						}
						sb.WriteString(indent + "    ]\n")
					} else {
						sb.WriteString(indent + "    " + leaf + "\n")
					}
				}
				writeNode(child, indent+"    ")
				sb.WriteString(indent + "}\n")
			}
		}
	}

	writeNode(root, "")

	return preamble + sb.String()
}

package core

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"github.com/srl-labs/srpls/utils"

	goyang "github.com/openconfig/goyang/pkg/yang"
	protocol "github.com/tliron/glsp/protocol_3_16"
)

var (
	intRe     = regexp.MustCompile(`^[+-]?\d+$`)
	uintRe    = regexp.MustCompile(`^\+?\d+$`)
	decimalRe = regexp.MustCompile(`^[+-]?\d+(\.\d+)?$`)
)

var AppName = "sr-lsp"

func ValidateKeyType(listEntry *goyang.Entry, keyVal string, lineNum, start, end uint32) *protocol.Diagnostic {
	if listEntry.Key == "" {
		return nil
	}
	keyLeaf, ok := listEntry.Dir[listEntry.Key]
	if !ok || keyLeaf == nil || keyLeaf.Type == nil {
		return nil
	}
	return ValidateValueAgainstType(keyLeaf.Type, keyVal, lineNum, start, end)
}

func ValidateLeafValue(entry *goyang.Entry, val string, lineNum, start, end uint32) *protocol.Diagnostic {
	if entry.Type == nil {
		return nil
	}
	return ValidateValueAgainstType(entry.Type, val, lineNum, start, end)
}

func ValidateValueAgainstType(t *goyang.YangType, val string, lineNum, start, end uint32) *protocol.Diagnostic {
	val = utils.StripQuotes(val)
	sev := protocol.DiagnosticSeverityError
	src := AppName

	if strings.TrimSpace(val) == "" {
		return &protocol.Diagnostic{
			Range: utils.DiagRange(lineNum, start, end), Severity: &sev, Source: &src,
			Message: "Value cannot be empty",
		}
	}

	switch t.Kind {
	case goyang.Yenum:
		if t.Enum != nil {
			for _, name := range t.Enum.Names() {
				if name == val {
					return nil
				}
			}
			return &protocol.Diagnostic{
				Range: utils.DiagRange(lineNum, start, end), Severity: &sev, Source: &src,
				Message: fmt.Sprintf("Invalid value '%s'; expected one of: %s", val, strings.Join(t.Enum.Names(), ", ")),
			}
		}

	case goyang.Ybool:
		if val != "true" && val != "false" {
			return &protocol.Diagnostic{
				Range: utils.DiagRange(lineNum, start, end), Severity: &sev, Source: &src,
				Message: fmt.Sprintf("Invalid value '%s'; expected 'true' or 'false'", val),
			}
		}

	case goyang.Yidentityref:
		if t.Enum != nil {
			for _, name := range t.Enum.Names() {
				if name == val {
					return nil
				}
			}
			return &protocol.Diagnostic{
				Range: utils.DiagRange(lineNum, start, end), Severity: &sev, Source: &src,
				Message: fmt.Sprintf("Invalid value '%s'", val),
			}
		}

	case goyang.Yunion:
		for _, member := range t.Type {
			if ValidateValueAgainstType(member, val, lineNum, start, end) == nil {
				return nil
			}
		}
		return &protocol.Diagnostic{
			Range: utils.DiagRange(lineNum, start, end), Severity: &sev, Source: &src,
			Message: fmt.Sprintf("Invalid value '%s'", val),
		}

	case goyang.Yint8, goyang.Yint16, goyang.Yint32, goyang.Yint64,
		goyang.Yuint8, goyang.Yuint16, goyang.Yuint32, goyang.Yuint64:
		return validateInteger(t, val, lineNum, start, end)

	case goyang.Ydecimal64:
		return validateDecimal64(t, val, lineNum, start, end)

	case goyang.Ystring:
		return validateString(t, val, lineNum, start, end)

	case goyang.Yempty:
		if val != "" {
			return &protocol.Diagnostic{
				Range: utils.DiagRange(lineNum, start, end), Severity: &sev, Source: &src,
				Message: fmt.Sprintf("Type 'empty' cannot have a value, got '%s'", val),
			}
		}

	case goyang.Ybits:
		if t.Bit != nil {
			for _, name := range t.Bit.Names() {
				if name == val {
					return nil
				}
			}
			return &protocol.Diagnostic{
				Range: utils.DiagRange(lineNum, start, end), Severity: &sev, Source: &src,
				Message: fmt.Sprintf("Invalid bit '%s'; expected one of: %s", val, strings.Join(t.Bit.Names(), ", ")),
			}
		}
	}
	return nil
}

func validateInteger(t *goyang.YangType, val string, lineNum, start, end uint32) *protocol.Diagnostic {
	sev := protocol.DiagnosticSeverityError
	src := AppName
	signed := t.Kind >= goyang.Yint8 && t.Kind <= goyang.Yint64

	if signed {
		if !intRe.MatchString(val) {
			return &protocol.Diagnostic{
				Range: utils.DiagRange(lineNum, start, end), Severity: &sev, Source: &src,
				Message: fmt.Sprintf("Invalid %s value '%s': not a valid integer", t.Kind, val),
			}
		}
	} else {
		if !uintRe.MatchString(val) {
			return &protocol.Diagnostic{
				Range: utils.DiagRange(lineNum, start, end), Severity: &sev, Source: &src,
				Message: fmt.Sprintf("Invalid %s value '%s': not a valid unsigned integer", t.Kind, val),
			}
		}
	}

	if len(t.Range) > 0 {
		var n goyang.Number
		if signed {
			i, err := strconv.ParseInt(val, 10, 64)
			if err != nil {
				return &protocol.Diagnostic{
					Range: utils.DiagRange(lineNum, start, end), Severity: &sev, Source: &src,
					Message: fmt.Sprintf("Invalid %s value '%s': %v", t.Kind, val, err),
				}
			}
			n = goyang.FromInt(i)
		} else {
			clean := strings.TrimPrefix(val, "+")
			u, err := strconv.ParseUint(clean, 10, 64)
			if err != nil {
				return &protocol.Diagnostic{
					Range: utils.DiagRange(lineNum, start, end), Severity: &sev, Source: &src,
					Message: fmt.Sprintf("Invalid %s value '%s': %v", t.Kind, val, err),
				}
			}
			n = goyang.FromUint(u)
		}
		if !utils.NumberInRange(n, t.Range) {
			return &protocol.Diagnostic{
				Range: utils.DiagRange(lineNum, start, end), Severity: &sev, Source: &src,
				Message: fmt.Sprintf("Value '%s' is out of range for %s (%s)", val, t.Kind, t.Range),
			}
		}
	}
	return nil
}

func validateDecimal64(t *goyang.YangType, val string, lineNum, start, end uint32) *protocol.Diagnostic {
	sev := protocol.DiagnosticSeverityError
	src := AppName

	if !decimalRe.MatchString(val) {
		return &protocol.Diagnostic{
			Range: utils.DiagRange(lineNum, start, end), Severity: &sev, Source: &src,
			Message: fmt.Sprintf("Invalid decimal64 value '%s': not a valid decimal number", val),
		}
	}

	if t.FractionDigits == 0 || len(t.Range) == 0 {
		return nil
	}

	n, err := goyang.ParseDecimal(val, uint8(t.FractionDigits))
	if err != nil {
		return &protocol.Diagnostic{
			Range: utils.DiagRange(lineNum, start, end), Severity: &sev, Source: &src,
			Message: fmt.Sprintf("Invalid decimal64 value '%s': %v", val, err),
		}
	}

	if !utils.NumberInRange(n, t.Range) {
		return &protocol.Diagnostic{
			Range: utils.DiagRange(lineNum, start, end), Severity: &sev, Source: &src,
			Message: fmt.Sprintf("Value '%s' is out of range for decimal64 (%s)", val, t.Range),
		}
	}
	return nil
}

func validateString(t *goyang.YangType, val string, lineNum, start, end uint32) *protocol.Diagnostic {
	sev := protocol.DiagnosticSeverityError
	src := AppName

	if len(t.Length) > 0 {
		strLen := goyang.FromUint(uint64(len(val)))
		if !utils.NumberInRange(strLen, t.Length) {
			return &protocol.Diagnostic{
				Range: utils.DiagRange(lineNum, start, end), Severity: &sev, Source: &src,
				Message: fmt.Sprintf("String length %d is out of allowed range (%s)", len(val), t.Length),
			}
		}
	}

	for _, pat := range t.POSIXPattern {
		re, err := regexp.Compile("^" + pat + "$")
		if err != nil {
			continue
		}
		if !re.MatchString(val) {
			return &protocol.Diagnostic{
				Range: utils.DiagRange(lineNum, start, end), Severity: &sev, Source: &src,
				Message: fmt.Sprintf("Value '%s' does not match pattern '%s'", val, pat),
			}
		}
	}

	// try use the pattern defined in the model
	// todo: actually use some xsd lib/transformer
	for _, pat := range t.Pattern {
		re, err := regexp.Compile("^" + pat + "$")
		if err != nil {
			continue
		}
		if !re.MatchString(val) {
			return &protocol.Diagnostic{
				Range: utils.DiagRange(lineNum, start, end), Severity: &sev, Source: &src,
				Message: fmt.Sprintf("Value '%s' does not match pattern '%s'", val, pat),
			}
		}
	}

	return nil
}

package core

import (
	"os"
	"regexp"
	"strconv"
	"strings"
)

var (
	rStyleRe  = regexp.MustCompile(`^(\d+)\.(\d+)\.R(\d+)$`)
	numericRe = regexp.MustCompile(`^(\d+)\.(\d+)\.(\d+)$`)
	bareRe    = regexp.MustCompile(`^(\d+)\.(\d+)$`)
)

type parsedVersion struct {
	Major, Minor, Revision int
	RStyle                 bool // maintenance release as an R prefix, ie. 25.10.R1 versus 25.10.1
	Raw                    string
}

func parseVersion(s string) (parsedVersion, bool) {
	if m := rStyleRe.FindStringSubmatch(s); m != nil {
		maj, _ := strconv.Atoi(m[1])
		min, _ := strconv.Atoi(m[2])
		rev, _ := strconv.Atoi(m[3])
		return parsedVersion{Major: maj, Minor: min, Revision: rev, RStyle: true, Raw: s}, true
	}
	if m := numericRe.FindStringSubmatch(s); m != nil {
		maj, _ := strconv.Atoi(m[1])
		min, _ := strconv.Atoi(m[2])
		rev, _ := strconv.Atoi(m[3])
		return parsedVersion{Major: maj, Minor: min, Revision: rev, RStyle: false, Raw: s}, true
	}
	if m := bareRe.FindStringSubmatch(s); m != nil {
		maj, _ := strconv.Atoi(m[1])
		min, _ := strconv.Atoi(m[2])
		return parsedVersion{Major: maj, Minor: min, Revision: -1, Raw: s}, true
	}
	return parsedVersion{}, false
}

func (v parsedVersion) sameMajorMinor(other parsedVersion) bool {
	return v.Major == other.Major && v.Minor == other.Minor
}

func (v parsedVersion) lessOrEqual(other parsedVersion) bool {
	if v.Revision == -1 {
		return true
	}
	if other.Revision == -1 {
		return false
	}
	return v.Revision <= other.Revision
}

// less returns true if v is strictly less than other across all components.
func (v parsedVersion) less(other parsedVersion) bool {
	if v.Major != other.Major {
		return v.Major < other.Major
	}
	if v.Minor != other.Minor {
		return v.Minor < other.Minor
	}
	// -1 (bare) < any explicit revision
	if v.Revision == -1 && other.Revision != -1 {
		return true
	}
	if v.Revision != -1 && other.Revision == -1 {
		return false
	}
	return v.Revision < other.Revision
}

// findLatestVersion scans parentDir for directories matching the given prefix
// and returns the highest version found (across all major.minors).
func findLatestVersion(parentDir, prefix string) (string, bool) {
	entries, err := os.ReadDir(parentDir)
	if err != nil {
		return "", false
	}

	var best parsedVersion
	found := false

	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		name := e.Name()
		if !strings.HasPrefix(name, prefix) {
			continue
		}
		verStr := strings.TrimPrefix(name, prefix)
		cand, ok := parseVersion(verStr)
		if !ok {
			continue
		}
		if !found || best.less(cand) {
			best = cand
			found = true
		}
	}

	if !found {
		return "", false
	}
	return best.Raw, true
}

func findFallbackVersion(parentDir, prefix, requested string) (string, bool) {
	req, ok := parseVersion(requested)
	if !ok {
		return "", false
	}

	entries, err := os.ReadDir(parentDir)
	if err != nil {
		return "", false
	}

	var best parsedVersion
	found := false

	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		name := e.Name()
		if !strings.HasPrefix(name, prefix) {
			continue
		}
		verStr := strings.TrimPrefix(name, prefix)
		cand, ok := parseVersion(verStr)
		if !ok || !cand.sameMajorMinor(req) || !cand.lessOrEqual(req) {
			continue
		}
		if !found || best.lessOrEqual(cand) {
			best = cand
			found = true
		}
	}

	if !found {
		return "", false
	}
	return best.Raw, true
}

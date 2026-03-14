package skills

import (
	"fmt"
	"strconv"
	"strings"
)

// ParseVersion parses a semantic version string like "1.2.3" or "v1.2.3"
// into a slice of integers. Pre-release suffixes (e.g. "-beta.1") are ignored.
func ParseVersion(s string) ([]int, error) {
	s = strings.TrimSpace(s)
	s = strings.TrimPrefix(s, "v")

	// Strip pre-release suffix (everything after first '-').
	if idx := strings.IndexByte(s, '-'); idx >= 0 {
		s = s[:idx]
	}

	if s == "" {
		return nil, fmt.Errorf("empty version string")
	}

	parts := strings.Split(s, ".")
	nums := make([]int, 0, len(parts))
	for _, p := range parts {
		n, err := strconv.Atoi(p)
		if err != nil {
			return nil, fmt.Errorf("invalid version component %q: %w", p, err)
		}
		if n < 0 {
			return nil, fmt.Errorf("negative version component %d", n)
		}
		nums = append(nums, n)
	}
	return nums, nil
}

// CompareVersions compares two version strings and returns:
//
//	-1 if a < b
//	 0 if a == b
//	+1 if a > b
//
// Missing components are treated as 0 (e.g. "1.2" == "1.2.0").
func CompareVersions(a, b string) int {
	va, errA := ParseVersion(a)
	vb, errB := ParseVersion(b)

	// Unparseable versions sort lower than parseable ones.
	if errA != nil && errB != nil {
		return 0
	}
	if errA != nil {
		return -1
	}
	if errB != nil {
		return 1
	}

	maxLen := len(va)
	if len(vb) > maxLen {
		maxLen = len(vb)
	}

	for i := 0; i < maxLen; i++ {
		ai, bi := 0, 0
		if i < len(va) {
			ai = va[i]
		}
		if i < len(vb) {
			bi = vb[i]
		}
		if ai < bi {
			return -1
		}
		if ai > bi {
			return 1
		}
	}
	return 0
}

// VersionIsNewer returns true if candidate is strictly newer than installed.
func VersionIsNewer(candidate, installed string) bool {
	return CompareVersions(candidate, installed) > 0
}

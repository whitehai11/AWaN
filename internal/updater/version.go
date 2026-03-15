package updater

import (
	"strconv"
	"strings"
)

// NormalizeVersion trims a leading "v" from semantic version strings.
func NormalizeVersion(version string) string {
	return strings.TrimPrefix(strings.TrimSpace(version), "v")
}

// CompareVersions compares two semantic version strings.
func CompareVersions(current, latest string) int {
	a := parseVersion(NormalizeVersion(current))
	b := parseVersion(NormalizeVersion(latest))

	for i := 0; i < len(a) || i < len(b); i++ {
		ai := 0
		bi := 0
		if i < len(a) {
			ai = a[i]
		}
		if i < len(b) {
			bi = b[i]
		}
		switch {
		case ai < bi:
			return -1
		case ai > bi:
			return 1
		}
	}

	return 0
}

func parseVersion(version string) []int {
	parts := strings.Split(version, ".")
	values := make([]int, 0, len(parts))
	for _, part := range parts {
		value, err := strconv.Atoi(strings.TrimSpace(part))
		if err != nil {
			values = append(values, 0)
			continue
		}
		values = append(values, value)
	}
	return values
}

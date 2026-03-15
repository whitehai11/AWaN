package plugins

import (
	"fmt"
	"strconv"
	"strings"
)

// CompareVersions compares semantic versions.
// It returns -1 when left < right, 0 when equal, and 1 when left > right.
func CompareVersions(left, right string) (int, error) {
	lv, err := parseVersion(left)
	if err != nil {
		return 0, err
	}
	rv, err := parseVersion(right)
	if err != nil {
		return 0, err
	}

	for i := 0; i < 3; i++ {
		switch {
		case lv[i] < rv[i]:
			return -1, nil
		case lv[i] > rv[i]:
			return 1, nil
		}
	}

	return 0, nil
}

func parseVersion(value string) ([3]int, error) {
	value = strings.TrimSpace(strings.TrimPrefix(value, "v"))
	if value == "" {
		return [3]int{}, fmt.Errorf("version cannot be empty")
	}

	mainPart := value
	if index := strings.IndexAny(mainPart, "+-"); index >= 0 {
		mainPart = mainPart[:index]
	}

	parts := strings.Split(mainPart, ".")
	if len(parts) > 3 {
		return [3]int{}, fmt.Errorf("invalid semantic version %q", value)
	}

	var parsed [3]int
	for i, part := range parts {
		n, err := strconv.Atoi(part)
		if err != nil || n < 0 {
			return [3]int{}, fmt.Errorf("invalid semantic version %q", value)
		}
		parsed[i] = n
	}

	return parsed, nil
}

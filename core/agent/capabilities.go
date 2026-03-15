package agent

import (
	"crypto/sha1"
	"encoding/hex"
	"sort"
	"strings"
)

// CapabilityContext is the minimal environment input used to derive an environment ID.
type CapabilityContext struct {
	Tools       []string
	Filesystem  bool
	Memory      bool
	Permissions []string
	Plugins     bool
}

// GenerateEnvironmentID returns a stable token-efficient environment identifier.
func GenerateEnvironmentID(ctx CapabilityContext) string {
	tools := append([]string(nil), ctx.Tools...)
	permissions := append([]string(nil), ctx.Permissions...)
	sort.Strings(tools)
	sort.Strings(permissions)

	payload := strings.Join([]string{
		"tools=" + strings.Join(tools, ","),
		"filesystem=" + boolString(ctx.Filesystem),
		"memory=" + boolString(ctx.Memory),
		"plugins=" + boolString(ctx.Plugins),
		"permissions=" + strings.Join(permissions, ","),
	}, "|")

	sum := sha1.Sum([]byte(payload))
	return hex.EncodeToString(sum[:])[:4]
}

func boolString(value bool) string {
	if value {
		return "true"
	}
	return "false"
}

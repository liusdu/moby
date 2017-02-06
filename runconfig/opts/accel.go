package opts

import (
	"regexp"
)

var nameExpStr = `[\w][\w\.-]*`
var rtExpStr = `[\w:\.-]+`
var nameExp = regexp.MustCompile(`^` + nameExpStr + `$`)
var rtExp = regexp.MustCompile(`^` + rtExpStr + `$`)

// ValidateAccelName checks whether accel name match name regexp
func ValidateAccelName(name string) bool {
	if name == "" {
		return true
	}
	return nameExp.MatchString(name)
}

// ValidateAccelRuntime  checks runtime with specified runtime regexp
func ValidateAccelRuntime(rt string) bool {
	if rt == "" {
		return true
	}
	return rtExp.MatchString(rt)
}

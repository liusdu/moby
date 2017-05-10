package opts

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/docker/engine-api/types/container"
)

// --accel args format: [<name>=]<runtime>[@<driver>[,<options>]]
var nameExpStr = `[\w][\w\.-]*`
var rtExpStr = `[\w:\.-]+`
var optExpStr = `(?:(?P<name>` + nameExpStr + `)=)?(?:(?P<runtime>` + rtExpStr + `)(?:@(?P<driver>[\w\.-]+)(?:,(?P<options>.*))?)?)`
var nameExp = regexp.MustCompile(`^` + nameExpStr + `$`)
var rtExp = regexp.MustCompile(`^` + rtExpStr + `$`)
var optExp = regexp.MustCompile(`^` + optExpStr + `$`)

// ValidatorAccelType defines a validator function that returns a validated struct and/or an error.
type ValidatorAccelType func(accel *container.AcceleratorConfig, accelerators []container.AcceleratorConfig) (*container.AcceleratorConfig, error)

// ValidateAccel validates that the specified string has a valid accel format.
func ValidateAccel(accel *container.AcceleratorConfig, accelerators []container.AcceleratorConfig) (*container.AcceleratorConfig, error) {
	// check duplicate name
	for _, cfg := range accelerators {
		if accel.Name != "" && cfg.Name == accel.Name {
			return nil, fmt.Errorf("Duplicated accelerator name: %s", accel.Name)
		}
	}
	return accel, nil
}

// parseAccelOpt validates that the specified string has a valid accel format.
func parseAccelOpt(accel string) (*container.AcceleratorConfig, error) {
	match := optExp.FindStringSubmatch(accel)
	if len(match) == 0 {
		return nil, fmt.Errorf("invalid options")
	}
	match = match[1:] // #1 is full match string
	keys := optExp.SubexpNames()[1:]
	copts := make(map[string]string)
	for i := 0; i < optExp.NumSubexp(); i = i + 1 {
		copts[keys[i]] = strings.TrimSpace(match[i])
	}

	// build AcceleratorConfig from options
	accelConfig := &container.AcceleratorConfig{
		IsPersistent: true,
		Name:         copts["name"],
		Runtime:      copts["runtime"],
		Driver:       copts["driver"],
		Options:      make([]string, 0),
	}
	if len(copts["options"]) > 0 {
		accelConfig.Options = strings.Split(copts["options"], ",")
	}

	return accelConfig, nil
}

// AccelOpt defines a map of Accel
type AccelOpt struct {
	values    []container.AcceleratorConfig
	validator ValidatorAccelType
}

// NewAccelOpt creates a new AccelOpt
func NewAccelOpt(validator ValidatorAccelType) AccelOpt {
	values := make([]container.AcceleratorConfig, 0)
	return AccelOpt{
		values:    values,
		validator: validator,
	}
}

// Set validates a Accel and sets its name as a key in AccelOpt
func (opt *AccelOpt) Set(val string) error {
	value, err := parseAccelOpt(val)
	if err != nil {
		return err
	}

	if opt.validator != nil {
		v, err := opt.validator(value, opt.GetAll())
		if err != nil {
			return err
		}
		value = v
	}

	opt.values = append(opt.values, *value)
	return nil
}

// String returns AccelOpt values as a string.
func (opt *AccelOpt) String() string {
	var out []string
	for _, v := range opt.values {
		out = append(out, fmt.Sprintf("%v", v))
	}

	return fmt.Sprintf("%v", out)
}

// GetAll returns map to Accels.
func (opt *AccelOpt) GetAll() []container.AcceleratorConfig {
	return opt.values
}

// Type returns the option type
func (opt *AccelOpt) Type() string {
	return "accelerator"
}

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

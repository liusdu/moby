package accelerator

import (
	"github.com/docker/engine-api/types"
	"github.com/docker/engine-api/types/filters"
)

var (
	// AcceptedFilters is an acceptable filters for validation
	// TODO: add status as a filter
	AcceptedFilters = map[string]bool{
		"driver":  true,
		"scope":   true,
		"name":    true,
		"id":      true,
		"owner":   true,
		"runtime": true,
	}
)

// filterAccels filters network list according to user specified filter
// and returns user chosen networks
func filterAccels(accels []*types.Accel, filter filters.Args) ([]*types.Accel, error) {
	// if filter is empty, return original network list
	if filter.Len() == 0 {
		return accels, nil
	}

	if err := filter.Validate(AcceptedFilters); err != nil {
		return nil, err
	}

	outputAccel := []*types.Accel{}
	for _, accel := range accels {
		if filter.Include("driver") {
			if !filter.ExactMatch("driver", accel.Driver) {
				continue
			}
		}
		if filter.Include("name") {
			if !filter.Match("name", accel.Name) {
				continue
			}
		}
		if filter.Include("id") {
			if !filter.Match("id", accel.ID) {
				continue
			}
		}
		if filter.Include("scope") {
			if !filter.ExactMatch("scope", accel.Scope) {
				continue
			}
		}
		if filter.Include("runtime") {
			if !filter.Match("runtime", accel.Runtime) {
				continue
			}
		}
		if filter.Include("owner") {
			if !filter.Match("owner", accel.Owner) {
				continue
			}
		}
		outputAccel = append(outputAccel, accel)
	}

	return outputAccel, nil
}

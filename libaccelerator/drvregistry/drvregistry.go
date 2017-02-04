// This file is ported from upstream libnetwork/drvregistry/drvregistry.go
// But have been adapted to accelerator.
package drvregistry

import (
	"fmt"
	"strings"
	"sync"

	"github.com/docker/docker/libaccelerator/driverapi"
)

type driverData struct {
	driver     driverapi.Driver
	capability driverapi.Capability
}

type driverTable map[string]*driverData

// DrvRegistry holds the registry of all accelerator drivers that it knows about.
type DrvRegistry struct {
	sync.Mutex
	drivers driverTable
	dfn     DriverNotifyFunc
}

// Functors definition

// InitFunc defines the driver initialization function signature.
type InitFunc func(driverapi.DriverCallback, map[string]interface{}) error

// DriverWalkFunc defines the network driver table walker function signature.
type DriverWalkFunc func(name string, driver driverapi.Driver, capability driverapi.Capability) bool

// DriverNotifyFunc defines the notify function signature when a new network driver gets registered.
type DriverNotifyFunc func(name string, driver driverapi.Driver, capability driverapi.Capability) error

// New retruns a new driver registry handler.
func New(lDs, gDs interface{}, dfn DriverNotifyFunc) (*DrvRegistry, error) {
	r := &DrvRegistry{
		drivers: make(driverTable),
		dfn:     dfn,
	}

	return r, nil
}

// AddDriver adds a network driver to the registry.
func (r *DrvRegistry) AddDriver(ntype string, fn InitFunc, config map[string]interface{}) error {
	return fn(r, config)
}

// WalkDrivers walks the accelerator drivers registered in the registry and invokes the passed walk function and each one of them.
func (r *DrvRegistry) WalkDrivers(dfn DriverWalkFunc) {
	type driverVal struct {
		name string
		data *driverData
	}

	r.Lock()
	dvl := make([]driverVal, 0, len(r.drivers))
	for k, v := range r.drivers {
		dvl = append(dvl, driverVal{name: k, data: v})
	}
	r.Unlock()

	for _, dv := range dvl {
		if dfn(dv.name, dv.data.driver, dv.data.capability) {
			break
		}
	}
}

// Driver returns the actual accelerator driver instance and its capability  which registered with the passed name.
func (r *DrvRegistry) Driver(name string) (driverapi.Driver, *driverapi.Capability) {
	r.Lock()
	defer r.Unlock()

	d, ok := r.drivers[name]
	if !ok {
		return nil, nil
	}

	return d.driver, &d.capability
}

// RegisterDriver registers the accelerator driver when it gets discovered.
func (r *DrvRegistry) RegisterDriver(name string, driver driverapi.Driver, capability driverapi.Capability) error {
	if strings.TrimSpace(name) == "" {
		return fmt.Errorf("accelerator name string cannot be empty")
	}

	r.Lock()
	_, ok := r.drivers[name]
	r.Unlock()

	if ok {
		return driverapi.ErrActiveRegistration(name)
	}

	if r.dfn != nil {
		if err := r.dfn(name, driver, capability); err != nil {
			return err
		}
	}

	dData := &driverData{driver, capability}

	r.Lock()
	r.drivers[name] = dData
	r.Unlock()

	return nil
}

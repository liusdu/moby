package libaccelerator

import (
	"fmt"
	"sync"

	log "github.com/Sirupsen/logrus"
	"github.com/docker/docker/libaccelerator/datastore"
	"github.com/docker/docker/libaccelerator/driverapi"
	"github.com/docker/docker/libaccelerator/drivers"
	"github.com/docker/docker/libaccelerator/drvregistry"
	"github.com/docker/docker/libaccelerator/types"
	"github.com/docker/docker/pkg/plugins"
	"github.com/docker/docker/pkg/stringid"
)

// AcceleratorController defines the interface type an accelerator
// controller should meet
type AcceleratorController interface {
	// ID provides a unique identity for the controller
	ID() string

	// Query is used to query an accelerator device for requested runtime
	//  - runtime: requested runtime
	//  - drivers: additional driver list
	// Return: driver name for this runtime
	Query(runtime string, driver string) (string, error)

	// AllocateContainerSlot is used to allocate an container scoped accelerator resource slot with the requested slot id, runtime and driver
	AllocateContainerSlot(sid string, runtime string, driver string, options ...string) (Slot, error)
	// AllocateGlobalSLot is used to allocate an global scoped accelerator resource slot, name is required
	AllocateGlobalSlot(name string, sid string, runtime string, driver string, options ...string) (Slot, error)

	// Slots returns the list of Slot(s) managed by this controller.
	Slots() []Slot

	// WalkSlots uses the provided function to walk the Slot(s) managed by this controller.
	WalkSlots(walker SlotWalker)

	// SlotByName returns the Slot which has the passwd name. If not found, the error ErrNoSuchSlot is returned.
	// Currently, only global-scoped Slot has name.
	SlotByName(name string) (Slot, error)

	// SlotById returns the Slot which has the passwd id. If not found, the error ErrNoSuchSlot is returned.
	SlotById(sid string) (Slot, error)

	// Stop is used to accelerator controller
	Stop()

	// WalkDrivers uses the provided function to walk the Driver(s) managed by this controller.
	WalkDrivers(walker drvregistry.DriverWalkFunc)

	// CleanupSlots do cleanup by syncing with driver first, then call user-provided `cleaner` fucntion
	CleanupSlots(cleaner SlotWalker)
}

// SlotWalker defines the handler type used to walk throught all the slot
type SlotWalker func(s Slot) bool

type controller struct {
	id          string
	drvRegistry *drvregistry.DrvRegistry
	stores      []datastore.DataStore
	sync.Mutex
}

// New creates a new AcceleratorController, which is used by daemon to mantain accelerator
func New(rootPath string) (AcceleratorController, error) {
	c := &controller{
		id: stringid.GenerateRandomID(),
	}

	if err := c.initStore(rootPath); err != nil {
		return nil, err
	}

	drvRegistry, err := drvregistry.New(nil, nil, c.RegisterDriver)
	if err != nil {
		return nil, err
	}
	c.drvRegistry = drvRegistry

	for _, i := range drivers.GetInitializers() {
		var dcfg map[string]interface{}
		if err := drvRegistry.AddDriver(i.DrvType, i.InitFn, dcfg); err != nil {
			log.Warnf("Failed to register accelerator driver \"%s\": %v", i.DrvType, err)
			continue
		}
	}

	return c, nil
}

// ID returns the unique id of the controller
func (c *controller) ID() string {
	return c.id
}

// Query will check whether driver support specified runtime
// if driverName not set, it will walk through all the drivers and return the one supported
func (c *controller) Query(runtime string, driverName string) (string, error) {
	// If no driver name, walk all drivers to match runtime
	if driverName == "" {
		c.drvRegistry.WalkDrivers(
			func(name string, drv driverapi.Driver, cap driverapi.Capability) bool {
				if checkRuntime(runtime, &cap) != nil {
					return false
				}
				driverName = name
				return true
			})
		if driverName == "" {
			return "", types.NotFoundErrorf("Query: runtime \"%s\" not support", runtime)
		}
	}

	// Make sure the requested driver is ready
	s := &slot{ctrlr: c, runtime: runtime, driverName: driverName}
	if d, err := s.driver(true); err != nil {
		return "", err
	} else if err := d.QueryRuntime(runtime); err != nil {
		return "", err
	}

	return driverName, nil
}

// AllocateGlobalSLot is used to allocate an global scoped accelerator resource slot, name is required
func (c *controller) AllocateGlobalSlot(name, sid, runtime, driver string, options ...string) (Slot, error) {
	if _, err := c.SlotByName(name); err == nil {
		return nil, SlotNameError(name)
	}
	s := &slot{
		name:       name,
		id:         sid,
		runtime:    runtime,
		driverName: driver,
		options:    options,
		ctrlr:      c,
		scope:      GlobalScope,
		state:      0,
	}
	return c.allocateSlot(s)
}

// AllocateContainerSlot is used to allocate an container scoped accelerator resource slot with the requested slot id, runtime and driver
func (c *controller) AllocateContainerSlot(sid, runtime, driver string, options ...string) (retS Slot, retErr error) {
	s := &slot{
		name:       "",
		id:         sid,
		runtime:    runtime,
		driverName: driver,
		options:    options,
		ctrlr:      c,
		scope:      ContainerScope,
		state:      0,
	}
	return c.allocateSlot(s)
}

func (c *controller) allocateSlot(s *slot) (retS *slot, retErr error) {
	if s.driverName == "" {
		return nil, fmt.Errorf("invalid driver \"%s\"", s.driverName)
	}

	// resolve the required driver
	_, cap, err := s.resolveDriver(s.driverName, true)
	if err != nil {
		return nil, err
	}
	// check runtime support
	if err := checkRuntime(s.runtime, cap); err != nil {
		return nil, err
	}

	// call driver
	if d, err := s.driver(true); err == nil {
		if err := d.AllocateSlot(s.id, s.runtime, s.options); err != nil {
			return nil, err
		}
		defer func(sid string) {
			if retErr != nil {
				d.ReleaseSlot(sid)
			}
		}(s.id)
	} else {
		return nil, err
	}

	// update to KV store
	if err = c.updateToStore(s); err != nil {
		return nil, err
	}

	return s, nil
}

func checkRuntime(runtime string, cap *driverapi.Capability) error {
	for _, rt := range cap.Runtimes {
		if runtime == rt {
			return nil
		}
	}
	return fmt.Errorf("checkRuntime: runtime \"%s\" not support", runtime)
}

// Slots returns the list of Slot(s) managed by this controller.
func (c *controller) Slots() []Slot {
	var list []Slot

	slots, err := c.getSlots()
	if err != nil {
		log.Error(err)
	}

	for _, s := range slots {
		if s.isInDelete() {
			continue
		}
		list = append(list, s)
	}

	return list
}

// WalkSlots uses the provided function to walk the Slot(s) managed by this controller.
func (c *controller) WalkSlots(walker SlotWalker) {
	for _, s := range c.Slots() {
		if walker(s) {
			return
		}
	}
}

// SlotByName returns the Slot which has the passwd name. If not found, the error ErrNoSuchSlot is returned.
// Currently, only global-scoped Slot has name.
func (c *controller) SlotByName(name string) (Slot, error) {
	// XXX: ContainerScope slots will get their name in future
	slots, err := c.getSlotsForScope(GlobalScope)
	if err != nil {
		return nil, err
	}

	for _, s := range slots {
		if s.Name() == name {
			return s, nil
		}
	}

	return nil, ErrNoSuchSlot(name)
}

// SlotById returns the Slot which has the passwd id. If not found, the error ErrNoSuchSlot is returned.
func (c *controller) SlotById(sid string) (Slot, error) {
	if sid == "" {
		return nil, ErrInvalidID(sid)
	}

	s, err := c.getSlot(sid)
	if err != nil {
		return nil, ErrNoSuchSlot(sid)
	}

	return s, nil
}

// Stop is used to accelerator controller
func (c *controller) Stop() {
	c.closeStores()
}

// WalkDrivers uses the provided function to walk the Driver(s) managed by this controller.
func (c *controller) WalkDrivers(walker drvregistry.DriverWalkFunc) {
	c.drvRegistry.WalkDrivers(walker)
}

// RegisterDriver is the callback function of docker plugin
func (c *controller) RegisterDriver(name string, driver driverapi.Driver, capability driverapi.Capability) error {
	log.Infof("Detect accelerator driver \"%s\", runtime support: %s", driver.Name(), capability.Runtimes)
	return nil
}

func (c *controller) loadDriver(driverName string) error {
	// Plugins pkg performs lazy loading of plugins that acts as remote drivers.
	// As per the design, this Get call will result in remote driver discovery if there is a corresponding plugin available.
	_, err := plugins.Get(driverName, driverapi.AcceleratorPluginEndpointType)
	if err != nil {
		if err == plugins.ErrNotFound {
			return types.NotFoundErrorf(err.Error())
		}
		return err
	}

	return nil
}

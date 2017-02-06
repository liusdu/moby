package daemon

import (
	"fmt"
	"strings"

	log "github.com/Sirupsen/logrus"
	"github.com/docker/docker/errors"
	"github.com/docker/docker/libaccelerator"
	"github.com/docker/docker/libaccelerator/driverapi"
	"github.com/docker/docker/pkg/namesgenerator"
	"github.com/docker/docker/pkg/plugins"
	"github.com/docker/docker/pkg/stringid"
	runconfigopts "github.com/docker/docker/runconfig/opts"
	"github.com/docker/engine-api/types"
)

// AcceleratorControllerEnabled shows whether accelerator controller is availible
func (daemon *Daemon) AcceleratorControllerEnabled() bool {
	return daemon.accelController != nil
}

func (daemon *Daemon) initAccelController(config *Config) (libaccelerator.AcceleratorController, error) {
	log.Debugf("Initialize accelerator controller")

	// Create libaccelerator controller
	controller, err := libaccelerator.New(daemon.root)
	if err != nil {
		return nil, fmt.Errorf("error obtaining controller instance: %v", err)
	}

	// Preload requested driver
	for _, name := range config.AccelDriverPlugins {
		log.Debugf("  Loading accelerator plugin: %s", name)
		_, err := plugins.Get(name, driverapi.AcceleratorPluginEndpointType)
		if err != nil {
			return nil, fmt.Errorf("error loading accelerator driver plugin \"%s\": %v", name, err)
		}
	}

	// Cleanup invalid accelerators
	controller.CleanupSlots(nil)

	return controller, nil
}

// AccelInspect looks up an accelerator by name, error occurs when accelrator not found
func (daemon *Daemon) AccelInspect(prefixOrName string) (*types.Accel, error) {
	slot, err := daemon.GetAccelSlot(prefixOrName)
	if err != nil {
		return nil, err
	}
	info := buildAccelType(slot)

	return info, nil
}

// Accels lists known accelerators
func (daemon *Daemon) Accels() ([]*types.Accel, []string, error) {
	accels := []*types.Accel{}
	l := func(slot libaccelerator.Slot) bool {
		accels = append(accels, buildAccelType(slot))
		return false
	}
	daemon.accelController.WalkSlots(l)

	return accels, nil, nil
}

// AccelDrivers lists known accelerator drivers
func (daemon *Daemon) AccelDrivers() ([]*types.AccelDriver, []string, error) {
	drvs := []*types.AccelDriver{}
	// TODO: detail driver desc
	l := func(name string, drv driverapi.Driver, cap driverapi.Capability) bool {
		// query devices of this driver
		infos := []string{}
		if devices, err := drv.ListDevice(); err == nil {
			for _, dev := range devices {
				infos = append(infos, fmt.Sprintf("%s\t%v", dev.DeviceIdentify, dev.SupportedRuntimes))
			}
		}
		desc := fmt.Sprintf("runtimes: %v\ndevices:\n\t%s", cap.Runtimes, strings.Join(infos, "\n\t"))
		drvs = append(drvs, &types.AccelDriver{Name: name, Desc: desc})
		return false
	}
	daemon.accelController.WalkDrivers(l)

	return drvs, nil, nil
}

// AccelDevices lists all accelerators managed by selected driver
func (daemon *Daemon) AccelDevices(driverName string) ([]types.AccelDevice, []string, error) {
	var drvInst driverapi.Driver = nil
	devices := make([]types.AccelDevice, 0)

	// get driver instance by name
	l := func(driver string, drv driverapi.Driver, _ driverapi.Capability) bool {
		if driver == driverName {
			drvInst = drv
			return true
		}
		return false
	}
	daemon.accelController.WalkDrivers(l)
	if drvInst == nil {
		return nil, nil, fmt.Errorf("no such driver")
	}

	// query driver for devices
	devInfos, err := drvInst.ListDevice()
	if err != nil {
		return nil, nil, err
	}

	// construct response
	for _, info := range devInfos {
		devices = append(devices, types.AccelDevice{
			SupportedRuntimes: info.SupportedRuntimes,
			DeviceIdentify:    info.DeviceIdentify,
			Capacity:          info.Capacity,
			Driver:            driverName,
			Status:            info.Status,
		})
	}

	return devices, nil, nil
}

// AccelCreate create accelerator with specified requests
func (daemon *Daemon) AccelCreate(name, driver, runtime string, options []string) (*types.Accel, error) {
	c := daemon.accelController
	if c == nil {
		log.Errorf("Un-initialized accelerator controller.")
		return nil, fmt.Errorf("internal error")
	}

	name = strings.TrimSpace(name)
	driver = strings.TrimSpace(driver)
	runtime = strings.TrimSpace(runtime)

	// validate runtime and name
	if len(runtime) == 0 {
		return nil, fmt.Errorf("empty runtime")
	} else if !runconfigopts.ValidateAccelRuntime(runtime) {
		return nil, fmt.Errorf("invalid accelerator runtime: %s\n", runtime)
	} else if len(name) != 0 && !runconfigopts.ValidateAccelName(name) {
		return nil, fmt.Errorf("invalid accelerator name: %s\n", name)
	}

	// driver is required for options
	if len(driver) == 0 && len(options) != 0 {
		return nil, fmt.Errorf("accelerator options must be specified along with driver")
	}

	// check runtime availibility
	queryDriver, err := c.Query(runtime, driver)
	if err != nil {
		return nil, err
	}

	// generate id and name if required
	sid, name, err := daemon.generateAccelIDAndName(name)
	if err != nil {
		return nil, err
	}

	// allocate global accelerator slot
	slot, err := c.AllocateGlobalSlot(name, sid, runtime, queryDriver, options...)
	if err != nil {
		return nil, err
	}

	return buildAccelType(slot), nil
}

// AccelRm remove specified accelerators
func (daemon *Daemon) AccelRm(prefixOrName string, force bool) error {
	slot, err := daemon.GetAccelSlot(prefixOrName)
	if err != nil {
		return err
	}

	// only global-scoped slots, a.k.a. user created slots, can be removed
	if slot.Scope() != libaccelerator.GlobalScope {
		return fmt.Errorf("can't release %s scope slot", slot.Scope())
	}
	// slot in use can not be removed
	if slot.Owner() != "" {
		return fmt.Errorf("slot in use")
	}

	// BADDRIVER slot can only be removed **forced**
	if slot.IsBadDriver() && !force {
		return fmt.Errorf("Remove a BADDRIVER slot may cause inconsistent of docker and accelerator plugin.\nTry to fix the plugin error and restart docker daemon to resolve BADDRIVER, or use \"--force\" to force remove BADDRIVER slot.")
	}

	// NODEV slot can only be removed **forced**
	if slot.IsNoDev() && !force {
		return fmt.Errorf("Remove a NODEV slot may cause inconsistent of docker and accelerator plugin.\nTry to fix the plugin error and restart docker daemon to resolve NODEV, or use \"--force\" to force remove NODEV slot.")
	}

	if err := slot.Release(); err != nil {
		return err
	}
	// TODO: how to deal with force?

	return nil
}

// GetAccelSlot returns slot info according to given prefix
func (daemon *Daemon) GetAccelSlot(prefixOrName string) (libaccelerator.Slot, error) {
	if len(prefixOrName) == 0 {
		return nil, errors.NewBadRequestError(fmt.Errorf("No slot name or ID supplied"))
	}
	// Find by Name
	s, err := daemon.GetAccelSlotByName(prefixOrName)
	if err != nil && !isNoSuchSlotError(err) {
		return nil, err
	}

	if s != nil {
		return s, nil
	}

	// Find by id
	return daemon.GetAccelSlotByID(prefixOrName)
}

func isNoSuchSlotError(err error) bool {
	_, ok := err.(libaccelerator.ErrNoSuchSlot)
	return ok
}

// GetAccelSlotByName returns slot info according to giver slot name
func (daemon *Daemon) GetAccelSlotByName(name string) (libaccelerator.Slot, error) {
	c := daemon.accelController
	if c == nil {
		return nil, libaccelerator.ErrNoSuchSlot(name)
	}

	fullName := name
	if name[0] != '/' {
		fullName = "/" + name
	}

	return c.SlotByName(fullName)
}

// GetAccelSlotByID return slot info according to given long or short slot id
func (daemon *Daemon) GetAccelSlotByID(partialID string) (libaccelerator.Slot, error) {
	list := daemon.GetAccelSlotsByID(partialID)

	if len(list) == 0 {
		return nil, libaccelerator.ErrNoSuchSlot(partialID)
	}
	if len(list) > 1 {
		return nil, libaccelerator.ErrInvalidID(partialID)
	}
	return list[0], nil
}

// Return all the matched slots info of given slot id
func (daemon *Daemon) GetAccelSlotsByID(partialID string) []libaccelerator.Slot {
	c := daemon.accelController
	if c == nil {
		return nil
	}
	list := []libaccelerator.Slot{}
	l := func(s libaccelerator.Slot) bool {
		if strings.HasPrefix(s.ID(), partialID) {
			list = append(list, s)
		}
		return false
	}
	c.WalkSlots(l)

	return list
}

func (daemon *Daemon) generateAccelIDAndName(name string) (string, string, error) {
	var err error

	id := stringid.GenerateNonCryptoID()
	if name == "" {
		if name, err = daemon.generateNewAccelName(id); err != nil {
			return "", "", err
		}
		return id, name, nil
	}

	if name, err = daemon.validAccelName(name); err != nil {
		return "", "", err
	}

	return id, name, nil
}

func (daemon *Daemon) generateNewAccelName(id string) (string, error) {
	var (
		name string
		err  error
	)

	for i := 0; i < 6; i++ {
		name = namesgenerator.GetRandomName(i)
		if name, err = daemon.validAccelName(name); err != nil {
			if _, ok := err.(libaccelerator.SlotNameError); ok {
				continue
			}
			return "", err
		}
		return name, nil
	}

	name = "/" + stringid.TruncateID(id)
	if name, err = daemon.validAccelName(name); err != nil {
		return "", err
	}

	return name, nil
}

func (daemon *Daemon) validAccelName(name string) (string, error) {
	if name[0] != '/' {
		name = "/" + name
	}
	if _, err := daemon.accelController.SlotByName(name); err != nil {
		if _, ok := err.(libaccelerator.ErrNoSuchSlot); ok {
			return name, nil
		}
		return "", err
	}
	return "", libaccelerator.SlotNameError(name)
}

func buildAccelType(slot libaccelerator.Slot) *types.Accel {
	accel := &types.Accel{
		ID:        slot.ID(),
		Name:      slot.Name(),
		Runtime:   slot.Runtime(),
		Driver:    slot.DriverName(),
		Device:    slot.Device(),
		Options:   slot.Options(),
		Owner:     slot.Owner(),
		Scope:     slot.Scope(),
		BadDriver: slot.IsBadDriver(),
		NoDevice:  slot.IsNoDev(),
	}
	if accel.Owner == "" {
		accel.State = "free"
	} else {
		accel.State = "used"
	}
	return accel
}

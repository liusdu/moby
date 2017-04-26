package daemon

import (
	"fmt"
	"strings"

	log "github.com/Sirupsen/logrus"
	"github.com/docker/docker/container"
	"github.com/docker/docker/image"
	"github.com/docker/docker/libaccelerator"
	"github.com/docker/docker/libaccelerator/driverapi"
	"github.com/docker/docker/pkg/plugins"
	"github.com/docker/docker/pkg/stringid"
	runconfigopts "github.com/docker/docker/runconfig/opts"
	containertypes "github.com/docker/engine-api/types/container"
)

// AcceleratorControllerEnabled shows whether accelerator controller is availible
func (daemon *Daemon) AcceleratorControllerEnabled() bool {
	return daemon.accelController != nil
}

func (daemon *Daemon) initAccelController(config *Config, activeAccelSlots map[string]string) (libaccelerator.AcceleratorController, error) {
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
	controller.CleanupSlots(func(s libaccelerator.Slot) bool {
		if owner, ok := activeAccelSlots[s.ID()]; ok {
			if s.Owner() != owner {
				if s.Owner() == "" || !daemon.Exists(s.Owner()) {
					s.SetOwner(owner)
				} else { // XXX bug here
					// invalid container
					log.Warnf(" ... invalid container %s", owner)
				}
			} else {
				// valid slot
				log.Debugf(" ... valid slot %s: driver %s", s.ID(), s.DriverName())
			}
			delete(activeAccelSlots, s.ID())
		} else {
			if s.Scope() == libaccelerator.ContainerScope {
				// unref container scope slot
				//  - mainly caused by daemon kill & restart without --live-restore,
				//    which will stop all running container but without accel cleanup,
				//    see releaseAccelResources()
				log.Warnf(" ... invalid slots %s: unref container-scope slot", s.ID())
				s.Release(true)
			} else {
				// valid slot
				log.Debugf(" ... valid slot %s: driver %s", s.ID(), s.DriverName())
				s.SetOwner("")
			}
		}

		return false
	})

	// TODO: Mark container with invalid accelerator slot?
	for _, cid := range activeAccelSlots {
		// invalid container
		log.Warnf(" ... invalid container %s", cid)
	}

	return controller, nil
}

func mergeAccelConfig(hostConfig *containertypes.HostConfig, img *image.Image) error {
	// when build image FROM scratch, img could be nil, this is to avoid nil dereference
	if img == nil || img.Config == nil || img.Config.Labels == nil {
		return nil
	}

	// Parse image accelerator runtime requirements
	//   - Label["runtime"] := "<name>=<runtime>;<name>=<runtime>;..."
	imgAccelConfigs := make([]containertypes.AcceleratorConfig, 0)
	if runtimeLabel, accelNeeded := img.Config.Labels["runtime"]; accelNeeded {
		var anonAccelNo = 0
		var anonAccelNamePrefix = "anon_img_accel_"

		for _, rt := range strings.Split(runtimeLabel, ";") {
			cfg := containertypes.AcceleratorConfig{
				Name:         "",
				Driver:       "",
				Options:      make([]string, 0),
				IsPersistent: false,
				Sid:          "",
			}
			spt := strings.Split(rt, "=")
			if len(spt) == 2 &&
				runconfigopts.ValidateAccelName(spt[0]) &&
				runconfigopts.ValidateAccelRuntime(spt[1]) {
				cfg.Name = spt[0]
				cfg.Runtime = spt[1]
			} else if len(spt) == 1 &&
				runconfigopts.ValidateAccelRuntime(spt[0]) {
				cfg.Name = fmt.Sprintf("%s%d", anonAccelNamePrefix, anonAccelNo)
				cfg.Runtime = spt[0]
				anonAccelNo = anonAccelNo + 1
			} else {
				return fmt.Errorf("Invalid runtime label: \"%s\"", runtimeLabel)
			}
			// FIXME: should image runtime LABEL support options?
			imgAccelConfigs = append(imgAccelConfigs, cfg)
		}
	}

	// Merge into HostConfig.Accelerators
	for _, imgAccelCfg := range imgAccelConfigs {
		var cAccelCfg containertypes.AcceleratorConfig
		bFound := false
		for _, cAccelCfg = range hostConfig.Accelerators {
			if cAccelCfg.Name == imgAccelCfg.Name {
				bFound = true
				break
			}
		}
		if bFound {
			// runtime must be exactly matched
			if cAccelCfg.Runtime != imgAccelCfg.Runtime {
				return fmt.Errorf("Unmatched accelerator binding for %s: runtime <%s> != <%s>", cAccelCfg.Name, cAccelCfg.Runtime, imgAccelCfg.Runtime)
			}
			driver := "*"
			if cAccelCfg.Driver != "" {
				driver = cAccelCfg.Driver
			}
			extra := ""
			if cAccelCfg.Sid != "" {
				extra = ", sid:" + cAccelCfg.Sid
			}
			log.Debugf("Bind image accelerator %s with persistent slot %s@<%s>%s", imgAccelCfg.Name, cAccelCfg.Runtime, driver, extra)
		} else {
			hostConfig.Accelerators = append(hostConfig.Accelerators, imgAccelCfg)
		}
	}

	return nil
}

func (daemon *Daemon) verifyAccelConfig(hostConfig *containertypes.HostConfig) error {
	// The Accelerators field in hostConfig maybe nil if no accelerator is required,
	// in which case, we just allocate an empty map for it.
	if hostConfig.Accelerators == nil {
		hostConfig.Accelerators = make([]containertypes.AcceleratorConfig, 0)
	}

	// Firstly, we check hostConfig.Accelerators filled by cli "--accel-runtime" options
	for idx := range hostConfig.Accelerators {
		accel := &hostConfig.Accelerators[idx]

		// validate parameters
		if !accel.IsPersistent || // accelerators from cli must "persistent"
			accel.Runtime == "" || // runtime must not empty, it can fill with either slot-name or *real* runtime
			accel.Name == "" || // accel can not have empty name
			// Runtime&Name must validate
			!runconfigopts.ValidateAccelRuntime(accel.Runtime) ||
			!runconfigopts.ValidateAccelName(accel.Name) {
			return fmt.Errorf("invalid accelerator config: %v", *accel)
		}
		// cli should not touch accel.Sid
		accel.Sid = ""
		// for Driver=="", it could be "name0=slot0" or "name0=runtime"
		if accel.Driver == "" {
			// search accel.Runtime as slot key (name/sid)
			key := accel.Runtime
			if slot, err := daemon.GetAccelSlot(key); err == nil {
				// only global-scoped slot can be assigned to container
				if slot.Scope() != libaccelerator.GlobalScope {
					return fmt.Errorf("not global scope slot")
				}
				// slot already bind to other container
				if slot.Owner() != "" {
					return fmt.Errorf("accel %s bind failed: slot %s already bind to %s", accel.Name, slot.Name(), slot.Owner())
				}
				// slot is not ready, e.g. BADDRV, NODEV
				if slot.IsBadDriver() {
					return fmt.Errorf("accel %s bind failed: invalid driver %s for slot %s", accel.Name, slot.DriverName(), slot.Name())
				} else if slot.IsNoDev() {
					return fmt.Errorf("accel %s bind failed: slot %s not exist", accel.Name, slot.Name())
				}
				// it should be "name0=slot0"
				accel.Sid = slot.ID()
				accel.Driver = slot.DriverName()
				accel.Runtime = slot.Runtime()
			} else {
				// it should be "name0=runtime" without driver, just leave it as is
			}
		}
	}

	return nil
}

func (daemon *Daemon) mergeAndVerifyAccelRuntime(hostConfig *containertypes.HostConfig, img *image.Image) (retErr error) {
	// Get accelerator controller
	c := daemon.accelController

	// Verify runtime from cli
	if err := daemon.verifyAccelConfig(hostConfig); err != nil {
		return err
	}

	// Merge image runtime requirements into HostConfig.Accelerators
	if err := mergeAccelConfig(hostConfig, img); err != nil {
		return err
	}

	// Check availiability of all accelerators
	for idx := range hostConfig.Accelerators {
		accel := &hostConfig.Accelerators[idx]

		// check availiability
		d, err := c.Query(accel.Runtime, accel.Driver)
		if err != nil {
			return err
		}
		// fixup persistent accelerator without driver
		//   e.g. "--accel name0=runtime"
		if accel.IsPersistent && accel.Driver == "" {
			accel.Driver = d
		}
	}

	return nil
}

func (daemon *Daemon) allocatePersistentAccelResources(container *container.Container) (retErr error) {
	// for container without accelerator, just return success
	if len(container.HostConfig.Accelerators) == 0 {
		return nil
	}
	// Get accelerator controller
	c := daemon.accelController

	log.Debugf("Allocate persistent accelerator resources for container \"%s\"", container.Name)
	for idx := range container.HostConfig.Accelerators {
		accel := &container.HostConfig.Accelerators[idx]

		if accel.IsPersistent && accel.Sid != "" {
			slot, err := c.SlotById(accel.Sid)
			if err != nil {
				return err
			}

			// Ignore slot already owned by container
			//  - this is for `update`, which means the slot is an un-touched
			//    slot in `update` action
			if slot.Owner() == container.ID {
				continue
			}

			// Set owner for slot-binding
			if slot.Owner() != "" {
				// XXX: we should never enter this
				// if some bug make us here, clear accel info to avoid further error
				accel.Sid = ""
				accel.Driver = ""
				return fmt.Errorf("Oops: this sould never happened: accel %s bind failed: slot %s already bind to %s", accel.Name, slot.Name(), slot.Owner())
			}
			slot.SetOwner(container.ID)

			defer func(slot libaccelerator.Slot) {
				if retErr != nil {
					slot.SetOwner("")
				}
			}(slot)
		} else if accel.IsPersistent && accel.Sid == "" {
			// Reserve resource for "persistent" && "!slot-binding" accelerator
			slot, err := c.AllocateContainerSlot(stringid.GenerateRandomID(), accel.Runtime, accel.Driver, accel.Options...)
			if err != nil {
				return err
			}
			slot.SetOwner(container.ID)
			accel.Sid = slot.ID()
			accel.Driver = slot.DriverName()

			defer func(accel *containertypes.AcceleratorConfig, slot libaccelerator.Slot) {
				if retErr != nil {
					accel.Sid = ""
					slot.Release(true)
				}
			}(accel, slot)
		}
	}
	return nil
}

func (daemon *Daemon) initializeAccelResources(container *container.Container) (retErr error) {
	// for container without accelerator, just return success
	if len(container.HostConfig.Accelerators) == 0 {
		return nil
	}
	log.Debugf("Initialize accelerator resources for container \"%s\"", container.Name)

	c := daemon.accelController

	for idx := range container.HostConfig.Accelerators {
		accel := &container.HostConfig.Accelerators[idx]

		// "persistent" accelerators are allocated at start stage
		if accel.IsPersistent {
			continue
		}

		// XXX Oops, if we kill all things(dockerd/containerd/shim), accel.Sid/Driver will still have values, how to deal with this?
		// If driver-plugin is also killed, we can just ignore this and re-allocate accel for it.
		// But if driver-plugin is still alive, we should just use this / or release it before re-allocate
		if accel.Driver != "" || accel.Sid != "" {
			log.Warnf("Unhandled non-empty accel.Driver/Sid when initialize accelerator resoruces for %s:", container.Name)
			log.Warnf("    name=%s", accel.Name)
			log.Warnf("    runtime=%s", accel.Runtime)
			log.Warnf("    *driver=<%s>*", accel.Driver)
			log.Warnf("    *sid=<%s>*", accel.Sid)
			log.Warnf("****** PLEASE CHECK THIS ******")
		}

		// Query driver
		driver, err := c.Query(accel.Runtime, accel.Driver)
		if err != nil {
			return err
		}

		slot, err := c.AllocateContainerSlot(stringid.GenerateRandomID(), accel.Runtime, driver, accel.Options...)
		if err != nil {
			return err
		}
		slot.SetOwner(container.ID)
		accel.Sid = slot.ID()
		accel.Driver = driver
		defer func(accel *containertypes.AcceleratorConfig, slot libaccelerator.Slot) {
			if retErr != nil {
				slot.Release(true)
				accel.Sid = ""
				accel.Driver = ""
			}
		}(accel, slot)
	}

	// Prepare accel resource, save volume/device/envs to configs
	mergedBindings := make(map[string]containertypes.AccelMount)
	mergedDevices := make(map[string]string)
	mergedEnvs := make(map[string]string)
	for _, accel := range container.HostConfig.Accelerators {
		slot, err := c.SlotById(accel.Sid)
		if err != nil {
			return err
		}
		binds, devices, envs, err := slot.Prepare()
		if err != nil {
			return err
		}

		// accel mount merge need container.Root
		if mergedBindings, err = mergeAccelMount(mergedBindings, binds, container.Root); err != nil {
			return err
		}
		// TODO: devices returned from slot.Prepare contains src only for now
		// will parse devices here later: src:dest:permission
		if mergedDevices, err = mergeAccelDevice(mergedDevices, devices); err != nil {
			return err
		}
		mergedEnvs = mergeAccelEnv(mergedEnvs, envs)
	}
	container.HostConfig.AccelBindings = mergedBindings
	container.HostConfig.AccelDevices = mergedDevices
	container.HostConfig.AccelEnvironments = mergedEnvs

	log.Debugf("AccelBings: %v", container.HostConfig.AccelBindings)
	log.Debugf("AccelDevices: %v", container.HostConfig.AccelDevices)
	log.Debugf("AccelEnvironments: %v", container.HostConfig.AccelEnvironments)

	return nil
}

func (daemon *Daemon) releaseAccelResources(container *container.Container, releasePersistent bool) error {
	// for container without accelerator, just return success
	if len(container.HostConfig.Accelerators) == 0 {
		return nil
	}
	log.Debugf("Release accelerator resources for container \"%s\" (releasePersistent:%t)", container.Name, releasePersistent)

	c := daemon.accelController

	// release volumebinding/devices/envs in container
	container.HostConfig.AccelBindings = nil
	container.HostConfig.AccelDevices = nil
	container.HostConfig.AccelEnvironments = nil

	// remove merged accel mounts
	removeMergedMounts(container.Root)

	// release accelerators
	for idx := range container.HostConfig.Accelerators {
		accel := &container.HostConfig.Accelerators[idx]

		if (releasePersistent || !accel.IsPersistent) && accel.Sid != "" {
			sid := accel.Sid
			// cleanup accelerator slot info
			accel.Sid = ""
			accel.Driver = ""
			// just continue if accelController is not initialized
			if c == nil {
				// only happened when daemon restart after kill
				log.Debugf("releaseAccelResources() called before accelController initialized, ignore driver operations")
				continue
			}

			// call driver to release
			if slot, err := c.SlotById(sid); err != nil {
				log.Debugf("unknown slot %s: %v", sid, err)
			} else {
				if slot.Scope() == libaccelerator.GlobalScope {
					// reset owner for global-scoped slot
					slot.SetOwner("")
				} else {
					// release slot for container-scoped slot
					slot.Release(true)
				}
			}
		}
	}

	return nil
}

func (daemon *Daemon) verifyAccelUpdateConfig(hostConfig *containertypes.HostConfig) error {
	if err := daemon.verifyAccelConfig(hostConfig); err != nil {
		return err
	}
	// check accel runtime availiability
	c := daemon.accelController
	for idx := range hostConfig.Accelerators {
		accelCfg := &hostConfig.Accelerators[idx]

		d, err := c.Query(accelCfg.Runtime, accelCfg.Driver)
		if err != nil {
			return err
		}
		// fixup persistent accelerator without driver
		//   e.g. "--accel name0=runtime"
		if accelCfg.IsPersistent && accelCfg.Driver == "" {
			accelCfg.Driver = d
		}
	}

	return nil
}

func (daemon *Daemon) updateAccelConfig(hostConfig *containertypes.HostConfig, container *container.Container) (retErr error) {
	updateAccelConfigs := hostConfig.Accelerators
	// make a copy of HostConfig.Accelerators for update
	cAccelConfigs := append([]containertypes.AcceleratorConfig{}, container.HostConfig.Accelerators...)
	oldSids := []string{} // record old sid of updated slot needs release
	updatedIdx := []int{} // record updated slot index for error rollback

	// no accelerator to update
	if len(updateAccelConfigs) == 0 {
		return nil
	}

	// update accelerators to local copy of HostConfig.Accelerators
	for _, updateAccelCfg := range updateAccelConfigs {
		var cAccelCfg containertypes.AcceleratorConfig
		bFound := false
		dst := 0
		for dst, cAccelCfg = range cAccelConfigs {
			if updateAccelCfg.Name == cAccelCfg.Name {
				bFound = true
				break
			}
		}
		if bFound {
			if !cAccelCfg.IsPersistent {
				return fmt.Errorf("Non-Persistent accelerator cannot be updated: %s", cAccelCfg.Name)
			} else if updateAccelCfg.Runtime != cAccelCfg.Runtime {
				return fmt.Errorf("runtime mismatch (%s: %s != %s)", cAccelCfg.Name, cAccelCfg.Runtime, updateAccelCfg.Runtime)
			}
			if cAccelCfg.Sid != "" {
				oldSids = append(oldSids, cAccelCfg.Sid)
			}
			cAccelConfigs[dst] = updateAccelCfg
			updatedIdx = append(updatedIdx, dst)
		} else {
			cAccelConfigs = append(cAccelConfigs, updateAccelCfg)
			updatedIdx = append(updatedIdx, len(cAccelConfigs)-1)
		}
	}

	// update container.HostConfig to new accelerator configs
	container.HostConfig.Accelerators = cAccelConfigs

	// allocate resources for updated accelerator slots
	//  - the non-updated slots will remain untouched
	//  - if allocate failed, the updated slots will be released
	if err := daemon.allocatePersistentAccelResources(container); err != nil {
		return err
	}

	// release updated slots if update failed
	defer func(updatedIdx []int) {
		// TODO
		// How to make unit-test or integration-test to cover following codes?
		//
		//   - Surely, we can remove /var/lib/docker/container/xxxx folder to
		//     force container.ToDisk() error, but this error will be caught in
		//     early stage of update process before this defer.
		//   - I have test this code by manual return error after this defer,
		//     and it works ok, so no further test required.
		if retErr != nil {
			slotIDs := []string{}
			for _, idx := range updatedIdx {
				slotIDs = append(slotIDs, cAccelConfigs[idx].Sid)
			}
			daemon.releaseSlotsByID(slotIDs)
		}
	}(updatedIdx)

	// save to disk
	if err := container.ToDisk(); err != nil {
		log.Errorf("Error saving updated container: %v", err)
		return err
	}

	log.Debugf("Updated acceleratros: %v", container.HostConfig.Accelerators)

	// release old accelerator
	daemon.releaseSlotsByID(oldSids)

	return nil
}

func (daemon *Daemon) releaseSlotsByID(slotIDs []string) {
	c := daemon.accelController
	for _, sid := range slotIDs {
		if slot, err := c.SlotById(sid); err != nil {
			continue
		} else {
			if slot.Scope() == libaccelerator.GlobalScope {
				slot.SetOwner("")
			} else {
				slot.Release(true)
			}
		}
	}
}

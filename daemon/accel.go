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
				Name:    "",
				Driver:  "",
				Options: make([]string, 0),
				Sid:     "",
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
	hostConfig.Accelerators = append(hostConfig.Accelerators, imgAccelConfigs...)

	return nil
}

func (daemon *Daemon) mergeAndVerifyAccelRuntime(hostConfig *containertypes.HostConfig, img *image.Image) (retErr error) {
	// Get accelerator controller
	c := daemon.accelController

	// Merge image runtime requirements into HostConfig.Accelerators
	if err := mergeAccelConfig(hostConfig, img); err != nil {
		return err
	}

	// Check availiability of all accelerators
	for idx, _ := range hostConfig.Accelerators {
		accel := &hostConfig.Accelerators[idx]

		// check availiability
		_, err := c.Query(accel.Runtime, accel.Driver)
		if err != nil {
			return err
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

	for idx, _ := range container.HostConfig.Accelerators {
		accel := &container.HostConfig.Accelerators[idx]

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
				slot.Release()
				accel.Sid = ""
				accel.Driver = ""
			}
		}(accel, slot)
	}

	return nil
}

func (daemon *Daemon) releaseAccelResources(container *container.Container) error {
	// for container without accelerator, just return success
	if len(container.HostConfig.Accelerators) == 0 {
		return nil
	}
	log.Debugf("Release accelerator resources for container \"%s\"", container.Name)

	c := daemon.accelController

	// release accelerators
	for idx, _ := range container.HostConfig.Accelerators {
		accel := &container.HostConfig.Accelerators[idx]

		if accel.Sid != "" {
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
					slot.Release()
				}
			}
		}
	}

	return nil
}

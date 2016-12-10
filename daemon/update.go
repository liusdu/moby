package daemon

import (
	"fmt"
	"strings"
	"time"

	"github.com/docker/docker/volume"
	"github.com/docker/engine-api/types/container"
)

// ContainerUpdate updates configuration of the container
func (daemon *Daemon) ContainerUpdate(name string, hostConfig *container.HostConfig) ([]string, error) {
	var warnings []string

	warnings, err := daemon.verifyContainerSettings(hostConfig, nil, true)
	if err != nil {
		return warnings, err
	}

	if err := daemon.update(name, hostConfig); err != nil {
		return warnings, err
	}

	return warnings, nil
}

// ContainerUpdateCmdOnBuild updates Path and Args for the container with ID cID.
func (daemon *Daemon) ContainerUpdateCmdOnBuild(cID string, cmd []string) error {
	if len(cmd) == 0 {
		return nil
	}
	c, err := daemon.GetContainer(cID)
	if err != nil {
		return err
	}
	c.Path = cmd[0]
	c.Args = cmd[1:]
	return nil
}

func (daemon *Daemon) update(name string, hostConfig *container.HostConfig) error {
	if hostConfig == nil {
		return nil
	}

	container, err := daemon.GetContainer(name)
	if err != nil {
		return err
	}

	restoreConfig := false
	backupHostConfig := *container.HostConfig
	backupMountPoints := container.MountPoints
	defer func() {
		if restoreConfig {
			container.Lock()
			container.HostConfig = &backupHostConfig
			container.MountPoints = backupMountPoints
			container.ToDisk()
			container.Unlock()
		}
	}()

	if container.RemovalInProgress || container.Dead {
		return errCannotUpdate(container.ID, fmt.Errorf("Container is marked for removal and cannot be \"update\"."))
	}

	if container.IsRunning() && hostConfig.KernelMemory != 0 {
		return errCannotUpdate(container.ID, fmt.Errorf("Can not update kernel memory to a running container, please stop it first."))
	}

	// Verify Device From Client.
	for _, device := range hostConfig.Resources.Devices {
		if _, _, err := getDevicesFromPath(device); err != nil {
			return errCannotUpdate(container.ID, err)
		}
	}

	container.Lock()
	// Remove binds from MountPoints.
	Mps := make(map[string]*volume.MountPoint)
	for k, v := range container.MountPoints {
		Mps[k] = v
	}
	for _, oldBinds := range container.HostConfig.Binds {
		found := false
		oldArr := strings.Split(oldBinds, ":")
		for _, bind := range hostConfig.Binds {
			bindArr := strings.Split(bind, ":")
			if oldArr[0] == bindArr[0] && oldArr[1] == bindArr[1] {
				found = true
			}
		}
		if !found {
			mp, err := volume.ParseMountSpec(oldBinds, "")
			if err != nil {
				container.Unlock()
				return errCannotUpdate(container.ID, err)
			}
			delete(Mps, mp.Destination)
		}
	}
	container.MountPoints = Mps
	container.Unlock()

	// Add new binds to MountPoints.
	if err := daemon.registerMountPoints(container, hostConfig); err != nil {
		restoreConfig = true
		return errCannotUpdate(container.ID, err)
	}

	if err := container.UpdateContainer(hostConfig); err != nil {
		restoreConfig = true
		return errCannotUpdate(container.ID, err)
	}

	// if Restart Policy changed, we need to update container monitor
	container.UpdateMonitor(hostConfig.RestartPolicy)

	// if container is restarting, wait 5 seconds until it's running
	if container.IsRestarting() {
		container.WaitRunning(5 * time.Second)
	}

	// If container is not running, update hostConfig struct is enough,
	// resources will be updated when the container is started again.
	// If container is running (including paused), we need to update configs
	// to the real world.
	if container.IsRunning() && !container.IsRestarting() {
		if err := daemon.containerd.UpdateResources(container.ID, toContainerdResources(hostConfig.Resources)); err != nil {
			restoreConfig = true
			return errCannotUpdate(container.ID, err)
		}
	}

	daemon.LogContainerEvent(container, "update")

	return nil
}

func errCannotUpdate(containerID string, err error) error {
	return fmt.Errorf("Cannot update container %s: %v", containerID, err)
}

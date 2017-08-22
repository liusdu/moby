package daemon

import (
	"fmt"

	"github.com/docker/docker/container"
	"github.com/docker/docker/daemon/freezer"
)

// ContainerUnpause unpauses a container
func (daemon *Daemon) ContainerUnpause(name string) error {
	container, err := daemon.GetContainer(name)
	if err != nil {
		return err
	}

	if err := daemon.containerUnpause(container); err != nil {
		return err
	}

	return nil
}

// containerUnpause resumes the container execution after the container is paused.
func (daemon *Daemon) containerUnpause(container *container.Container) error {
	container.Lock()
	defer container.Unlock()

	// We cannot unpause the container which is not running
	if !container.Running {
		return errNotRunning{container.ID}
	}

	// We cannot unpause the container which is not paused
	if !container.Paused {
		return fmt.Errorf("Container %s is not paused", container.ID)
	}

	if daemon.IsNativeContainer(container) {
		freezer, err := freezer.New(container.ID, container.CgroupParent, UsingSystemd(daemon.configStore))
		if err != nil {
			return fmt.Errorf("Failed to create freezer for container %s: %v", container.ID, err)
		}
		if err := freezer.Resume(); err != nil {
			return fmt.Errorf("Cannot unpause container %s: %s", container.ID, err)
		}
		container.Paused = false
		daemon.LogContainerEvent(container, "unpause")
	} else {
		if err := daemon.containerd.Resume(container.ID); err != nil {
			return fmt.Errorf("Cannot unpause container %s: %s", container.ID, err)
		}
	}
	return nil
}

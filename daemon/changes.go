package daemon

import (
	"fmt"

	"github.com/docker/docker/pkg/archive"
)

// ContainerChanges returns a list of container fs changes
func (daemon *Daemon) ContainerChanges(name string) ([]archive.Change, error) {
	container, err := daemon.GetContainer(name)
	if err != nil {
		return nil, err
	}

	if container.HostConfig.ExternalRootfs != "" {
		return nil, fmt.Errorf("can't diff a container with external rootfs")
	}

	container.Lock()
	defer container.Unlock()
	return daemon.changes(container)
}

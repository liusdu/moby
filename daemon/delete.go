package daemon

import (
	"fmt"
	"os"
	"path"
	"strings"

	"github.com/Sirupsen/logrus"
	"github.com/docker/docker/container"
	"github.com/docker/docker/errors"
	"github.com/docker/docker/layer"
	"github.com/docker/docker/pkg/system"
	volumestore "github.com/docker/docker/volume/store"
	"github.com/docker/engine-api/types"
)

// ContainerRm removes the container id from the filesystem. An error
// is returned if the container is not found, or if the remove
// fails. If the remove succeeds, the container name is released, and
// network links are removed.
func (daemon *Daemon) ContainerRm(name string, config *types.ContainerRmConfig) error {
	container, err := daemon.GetContainer(name)
	if err != nil {
		return err
	}

	// Container state RemovalInProgress should be used to avoid races.
	if inProgress := container.SetRemovalInProgress(); inProgress {
		return nil
	}
	defer container.ResetRemovalInProgress()

	// check if container wasn't deregistered by previous rm since Get
	if c := daemon.containers.Get(container.ID); c == nil {
		return nil
	}

	if config.RemoveLink {
		return daemon.rmLink(container, name)
	}

	return daemon.cleanupContainer(container, config.ForceRemove, config.RemoveVolume)
}

func (daemon *Daemon) rmLink(container *container.Container, name string) error {
	if name[0] != '/' {
		name = "/" + name
	}
	parent, n := path.Split(name)
	if parent == "/" {
		return fmt.Errorf("Conflict, cannot remove the default name of the container")
	}

	parent = strings.TrimSuffix(parent, "/")
	pe, err := daemon.nameIndex.Get(parent)
	if err != nil {
		return fmt.Errorf("Cannot get parent %s for name %s", parent, name)
	}

	daemon.releaseName(name)
	parentContainer, _ := daemon.GetContainer(pe)
	if parentContainer != nil {
		daemon.linkIndex.unlink(name, container, parentContainer)
		if err := daemon.updateNetwork(parentContainer); err != nil {
			logrus.Debugf("Could not update network to remove link %s: %v", n, err)
		}
	}
	return nil
}

// cleanupContainer unregisters a container from the daemon, stops stats
// collection and cleanly removes contents and metadata from the filesystem.
func (daemon *Daemon) cleanupContainer(container *container.Container, forceRemove, removeVolume bool) (err error) {
	if container.IsRunning() {
		if !forceRemove {
			err := fmt.Errorf("You cannot remove a running container %s. Stop the container before attempting removal or use -f", container.ID)
			return errors.NewRequestConflictError(err)
		}
		if err := daemon.Kill(container); err != nil {
			return fmt.Errorf("Could not kill running container %s, cannot remove - %v", container.ID, err)
		}
	}

	// stop collection of stats for the container regardless
	// if stats are currently getting collected.
	daemon.statsCollector.stopCollection(container)

	if err = daemon.containerStop(container, 3); err != nil {
		return err
	}

	// Mark container dead. We don't want anybody to be restarting it.
	container.SetDead()

	// Save container state to disk. So that if error happens before
	// container meta file got removed from disk, then a restart of
	// docker should not make a dead container alive.
	if err := container.ToDiskLocking(); err != nil && !os.IsNotExist(err) {
		logrus.Errorf("Error saving dying container to disk: %v", err)
	}

	// Release accelerator resources
	if err = daemon.releaseAccelResources(container, true); err != nil {
		return fmt.Errorf("Unable to release accelerator resources: %v", err)
	}

	// When container creation fails and `RWLayer` has not been created yet, we
	// do not call `ReleaseRWLayer`
	if container.RWLayer != nil {
		metadata, err := daemon.layerStore.ReleaseRWLayer(container.RWLayer)
		layer.LogReleaseMetadata(metadata)
		if err != nil && err != layer.ErrMountDoesNotExist {
			return fmt.Errorf("Driver %s failed to remove root filesystem %s: %s", daemon.GraphDriverName(), container.ID, err)
		}
	}

	if err := system.EnsureRemoveAll(container.Root); err != nil {
		return fmt.Errorf("unable to remove filesystem for %s: %v", container.ID, err)
	}

	daemon.nameIndex.Delete(container.ID)
	daemon.linkIndex.delete(container)
	selinuxFreeLxcContexts(container.ProcessLabel)
	daemon.idIndex.Delete(container.ID)
	daemon.containers.Delete(container.ID)
	if e := daemon.removeMountPoints(container, removeVolume); e != nil {
		logrus.Error(e)
	}
	daemon.LogContainerEvent(container, "destroy")
	return nil
}

// VolumeRm removes the volume with the given name.
// If the volume is referenced by a container it is not removed
// This is called directly from the remote API
func (daemon *Daemon) VolumeRm(name string) error {
	v, err := daemon.volumes.Get(name)
	if err != nil {
		return err
	}

	if err := daemon.volumes.Remove(v); err != nil {
		if volumestore.IsInUse(err) {
			err := fmt.Errorf("Unable to remove volume, volume still in use: %v", err)
			return errors.NewRequestConflictError(err)
		}
		return fmt.Errorf("Error while removing volume %s: %v", name, err)
	}
	daemon.LogVolumeEvent(v.Name(), "destroy", map[string]string{"driver": v.DriverName()})
	return nil
}

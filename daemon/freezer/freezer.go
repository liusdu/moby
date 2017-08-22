package freezer

import (
	"bytes"
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/opencontainers/runc/libcontainer/configs"
	"github.com/opencontainers/runc/libcontainer/utils"
)

// Freezer is the interface which could be used to pause/resume container,
// And it could be used to get the real container paused status of a container too.
type Freezer interface {
	// Pause will set the container to pause state by writing freeze cgroup.
	Pause() error

	// Resume will set the container to running state by writing freeze cgroup.
	Resume() error

	// IsPaused will return if the container is paused or not by reading cgroup information.
	IsPaused() (bool, error)
}

func writeFile(dir, file, data string) error {
	// Normally dir should not be empty, one case is that cgroup subsystem
	// is not mounted, we will get empty dir, and we want it fail here.
	if dir == "" {
		return fmt.Errorf("no such directory for %s", file)
	}
	if err := ioutil.WriteFile(filepath.Join(dir, file), []byte(data), 0700); err != nil {
		return fmt.Errorf("failed to write %v to %v: %v", data, file, err)
	}
	return nil
}

func readFile(dir, file string) (string, error) {
	data, err := ioutil.ReadFile(filepath.Join(dir, file))
	return string(data), err
}

// New will create a Freezer interface for caller
func New(cid, cgroupParent string, useSystemdCgroup bool) (Freezer, error) {
	if useSystemdCgroup && !couldUseSystemd {
		return nil, fmt.Errorf("systemd cgroup flag passed, but systemd support for managing cgroups is not available")
	}
	cgroupConfig, err := prepareCgroupConfig(cid, cgroupParent, useSystemdCgroup)
	if err != nil {
		return nil, err
	}

	return newFreezer(useSystemdCgroup, cgroupConfig)
}

func prepareCgroupConfig(cid, cgroupsPath string, useSystemdCgroup bool) (*configs.Cgroup, error) {
	var myCgroupPath string
	c := &configs.Cgroup{
		Resources: &configs.Resources{},
	}
	if cgroupsPath != "" {
		myCgroupPath = utils.CleanPath(cgroupsPath)
		if useSystemdCgroup {
			myCgroupPath = cgroupsPath
		}
	}

	if useSystemdCgroup {
		if myCgroupPath == "" {
			c.Parent = "system.slice"
			c.ScopePrefix = "runc"
			c.Name = cid
		} else {
			// Parse the path from expected "slice:prefix:name"
			// for e.g. "system.slice:docker:1234"
			parts := strings.Split(myCgroupPath, ":")
			if len(parts) != 3 {
				return nil, fmt.Errorf("expected cgroupsPath to be of format \"slice:prefix:name\" for systemd cgroups")
			}
			c.Parent = parts[0]
			c.ScopePrefix = parts[1]
			c.Name = parts[2]
		}
	} else {
		if myCgroupPath == "" {
			c.Name = cid
		}
		c.Path = myCgroupPath
	}
	return c, nil
}

func newFreezer(useSystemdCgroup bool, cgroup *configs.Cgroup) (Freezer, error) {
	var err error
	var path string

	if useSystemdCgroup {
		path, err = systemdCgroupPath("freezer", cgroup)
		if err != nil {
			return nil, err
		}
	} else {
		path, err = fsCgroupPath("freezer", cgroup)
		if err != nil {
			return nil, err
		}
	}
	return &freezer{path: path}, nil
}

type freezer struct {
	sync.Mutex
	path string
}

// Pause will set the container to pause state by writing freeze cgroup.
func (f *freezer) Pause() error {
	f.Lock()
	defer f.Unlock()

	if err := f.updateCgroup(string(configs.Frozen)); err != nil {
		return err
	}

	tasks, err := readFile(f.path, "tasks")
	if err != nil {
		return fmt.Errorf("failed to check container cgroup task status: %v", err)
	}

	if strings.TrimSpace(tasks) == "" {
		return fmt.Errorf("error: no tasks running in freeze cgroup")
	}
	return nil
}

// Resume will set the container to running state by writing freeze cgroup.
func (f *freezer) Resume() error {
	f.Lock()
	defer f.Unlock()
	return f.updateCgroup(string(configs.Thawed))
}

// IsPaused will return if the container is paused or not by reading cgroup information.
func (f *freezer) IsPaused() (bool, error) {
	f.Lock()
	defer f.Unlock()

	data, err := readFile(f.path, "freezer.state")
	if err != nil {
		// If freezer cgroup is not mounted, the container would just be not paused.
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, fmt.Errorf("failed to check container status: %v", err)
	}
	return bytes.Equal(bytes.TrimSpace([]byte(data)), []byte("FROZEN")), nil
}

func (f *freezer) updateCgroup(state string) error {
	if err := writeFile(f.path, "freezer.state", state); err != nil {
		return err
	}

	for {
		newState, err := readFile(f.path, "freezer.state")
		if err != nil {
			return err
		}
		if strings.TrimSpace(newState) == state {
			break
		}
		time.Sleep(1 * time.Millisecond)
	}
	return nil
}

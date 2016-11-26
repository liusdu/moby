// +build !seccomp,!windows

package daemon

import (
	"github.com/docker/docker/container"
	"github.com/opencontainers/runtime-spec/specs-go"
)

func setSeccomp(daemon *Daemon, rs *specs.Spec, c *container.Container) error {
	return nil
}

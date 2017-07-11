// +build !windows

package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/docker/docker/pkg/integration/checker"
	"github.com/docker/engine-api/types"
	containertypes "github.com/docker/engine-api/types/container"
	"github.com/go-check/check"
)

func (s *DockerAccelSuite) TestDockerAccelListDevice(c *check.C) {
	// Active plugin by creating an accel
	dockerCmd(c, "accel", "create", "--driver", dummyAccelDriver, "--runtime", fakeRuntime, "tmpacc")
	dockerCmd(c, "accel", "rm", "tmpacc")

	code, data, err := sockRequest("GET", "/accelerators/devices", nil)
	c.Assert(err, checker.IsNil)
	c.Assert(code, checker.Equals, http.StatusOK)

	resp := types.AccelDevicesResponse{}
	err = json.Unmarshal(data, &resp.Devices)
	c.Assert(err, checker.IsNil)
	c.Assert(len(resp.Devices), checker.GreaterOrEqualThan, 1)
	for _, dev := range resp.Devices {
		if len(dev.SupportedRuntimes) == 1 &&
			dev.SupportedRuntimes[0] == accelDevice.SupportedRuntimes[0] {
			c.Assert(dev, checker.DeepEquals, accelDevice)
			return
		}
	}
	c.Fatalf("%s not in /accelerators/devices: %v", accelDevice.Device, resp.Devices)
}

func (s *DockerAccelSuite) TestDockerAccelListDeviceByDriver(c *check.C) {
	// Active plugin by creating an accel
	dockerCmd(c, "accel", "create", "--driver", dummyAccelDriver, "--runtime", fakeRuntime, "tmpacc")
	dockerCmd(c, "accel", "rm", "tmpacc")

	endpoint := fmt.Sprintf("/accelerators/drivers/%s/devices", dummyAccelDriver)
	code, data, err := sockRequest("GET", endpoint, nil)
	c.Assert(err, checker.IsNil)
	c.Assert(code, checker.Equals, http.StatusOK)

	resp := types.AccelDevicesResponse{}
	err = json.Unmarshal(data, &resp)
	c.Assert(err, checker.IsNil)
	c.Assert(len(resp.Devices), checker.Equals, 1)
	c.Assert(resp.Devices, checker.DeepEquals, []types.AccelDevice{accelDevice})
}

func (s *DockerAccelSuite) TestDockerAccelApiContainerWithoutPersistent(c *check.C) {
	name := "accel_without_persistent"
	config := map[string]interface{}{
		"Image": "busybox",
		"Cmd":   []string{"top"},
		"HostConfig": map[string]interface{}{
			"Accelerators": []containertypes.AcceleratorConfig{
				{
					Name:         "a0",
					Runtime:      fakeRuntime,
					Driver:       dummyAccelDriver,
					Options:      []string{},
					IsPersistent: false,
					Sid:          "",
				},
			},
		},
	}

	status, _, err := sockRequest("POST", "/containers/create?name="+name, config)
	c.Assert(err, checker.IsNil)
	c.Assert(status, checker.Equals, http.StatusCreated)
	defer dockerCmd(c, "rm", name)

	out, _ := dockerCmd(c, "inspect", "--format", "{{(index .HostConfig.Accelerators 0).Sid}}", name)
	c.Assert(strings.TrimSpace(out), checker.HasLen, 0)

	dockerCmd(c, "start", name)
	out, _ = dockerCmd(c, "inspect", "--format", "{{(index .HostConfig.Accelerators 0).Sid}}", name)
	c.Assert(len(strings.TrimSpace(out)), checker.GreaterThan, 0)

	dockerCmd(c, "kill", name)
	out, _ = dockerCmd(c, "inspect", "--format", "{{(index .HostConfig.Accelerators 0).Sid}}", name)
	c.Assert(strings.TrimSpace(out), checker.HasLen, 0)
}

// +build !windows

package main

import (
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/docker/docker/pkg/integration/checker"
	"github.com/docker/engine-api/types"
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

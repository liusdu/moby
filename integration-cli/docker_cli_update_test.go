package main

import (
	"os/exec"
	"strings"
	"time"

	"github.com/docker/docker/pkg/integration/checker"
	"github.com/go-check/check"
)

func (s *DockerSuite) TestUpdateRestartPolicy(c *check.C) {
	out, _ := dockerCmd(c, "run", "-d", "--restart=on-failure:3", "busybox", "sh", "-c", "sleep 1 && false")
	timeout := 60 * time.Second
	if daemonPlatform == "windows" {
		timeout = 180 * time.Second
	}

	id := strings.TrimSpace(string(out))

	// update restart policy to on-failure:5
	dockerCmd(c, "update", "--restart=on-failure:5", id)

	err := waitExited(id, timeout)
	c.Assert(err, checker.IsNil)

	count := inspectField(c, id, "RestartCount")
	c.Assert(count, checker.Equals, "5")

	maximumRetryCount := inspectField(c, id, "HostConfig.RestartPolicy.MaximumRetryCount")
	c.Assert(maximumRetryCount, checker.Equals, "5")
}

func (s *DockerSuite) TestUpdateNotAffectMonitorRestartPolicy(c *check.C) {
	testRequires(c, cpuShare)

	out, _ := dockerCmd(c, "run", "-tid", "--restart=always", "busybox", "sh")
	id := strings.TrimSpace(string(out))
	dockerCmd(c, "update", "--cpu-shares", "512", id)

	cmd := exec.Command(dockerBinary, "attach", id)
	stdin, err := cmd.StdinPipe()
	if err != nil {
		c.Fatal(err)
	}
	defer stdin.Close()
	c.Assert(cmd.Start(), check.IsNil)
	defer cmd.Process.Kill()

	_, err = stdin.Write([]byte("exit\n"))
	c.Assert(err, check.IsNil)

	// container should restart again
	c.Assert(waitRun(id), checker.IsNil)
}

// +build daemon,!windows,!experimental

package main

import (
	"os"
	"strings"

	"github.com/go-check/check"
)

// #22913
func (s *DockerDaemonSuite) TestContainerStartAfterDaemonKill(c *check.C) {
	testRequires(c, DaemonIsLinux)
	c.Assert(s.d.StartWithBusybox(), check.IsNil)

	// the application is chosen so it generates output and doesn't react to SIGTERM
	out, err := s.d.Cmd("run", "-d", "busybox", "sh", "-c", "while true;do date;done")
	c.Assert(err, check.IsNil, check.Commentf("Output: %s", out))
	id := strings.TrimSpace(out)
	c.Assert(s.d.cmd.Process.Signal(os.Kill), check.IsNil)

	// restart daemon.
	if err := s.d.Restart(); err != nil {
		c.Fatal(err)
	}

	out, err = s.d.Cmd("start", id)
	c.Assert(err, check.IsNil, check.Commentf("Output: %s", out))
}

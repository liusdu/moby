package daemon

import (
	"errors"
	"fmt"
	"runtime"
	"strconv"
	"time"

	"github.com/Sirupsen/logrus"
	"github.com/docker/docker/libcontainerd"
	"github.com/docker/docker/restartmanager"
)

// StateChanged updates daemon state changes from containerd
func (daemon *Daemon) StateChanged(id string, e libcontainerd.StateInfo) error {
	c := daemon.containers.Get(id)
	if c == nil {
		return fmt.Errorf("no such container: %s", id)
	}

	switch e.State {
	case libcontainerd.StateOOM:
		// StateOOM is Linux specific and should never be hit on Windows
		if runtime.GOOS == "windows" {
			return errors.New("Received StateOOM from libcontainerd on Windows. This should never happen.")
		}
		daemon.LogContainerEvent(c, "oom")
	case libcontainerd.StateExit:

		c.Lock()
		defer c.Unlock()
		c.StreamConfig.Wait()
		c.Reset(false)

		// If daemon is being shutdown, don't let the container restart
		restart, wait, err := c.RestartManager().ShouldRestart(e.ExitCode, daemon.IsShuttingDown(), time.Since(c.StartedAt))
		if err == nil && restart {
			c.RestartCount++
			c.SetRestarting(platformConstructExitStatus(e))
		} else {
			c.SetStopped(platformConstructExitStatus(e))
		}

		attributes := map[string]string{
			"exitCode": strconv.Itoa(int(e.ExitCode)),
		}
		daemon.LogContainerEventWithAttributes(c, "die", attributes)
		daemon.Cleanup(c)

		if err == nil && restart {
			go func() {
				err := <-wait
				daemon.waitForStartupDone()
				if err == nil {
					if err = daemon.containerStart(c, false); err != nil {
						logrus.Debugf("failed to restart container: %+v", err)
					}
				}
				if err != nil {
					c.SetStopped(platformConstructExitStatus(e))
					if err != restartmanager.ErrRestartCanceled {
						logrus.Errorf("restartmanger wait error: %+v", err)
					}
				}
			}()
		}

		if err := c.ToDisk(); err != nil {
			return err
		}
		return daemon.postRunProcessing(c, e)

	case libcontainerd.StateExitProcess:
		if execConfig := c.ExecCommands.Get(e.ProcessID); execConfig != nil {
			ec := int(e.ExitCode)
			execConfig.Lock()
			defer execConfig.Unlock()
			execConfig.ExitCode = &ec
			execConfig.Running = false
			execConfig.StreamConfig.Wait()
			if err := execConfig.CloseStreams(); err != nil {
				logrus.Errorf("failed to cleanup exec %s streams: %s", c.ID, err)
			}

			// remove the exec command from the container's store only and not the
			// daemon's store so that the exec command can be inspected.
			c.ExecCommands.Delete(execConfig.ID)
		} else {
			logrus.Warnf("Ignoring StateExitProcess for %v but no exec command found", e)
		}
	case libcontainerd.StateStart, libcontainerd.StateRestore:
		c.SetRunning(int(e.Pid), e.State == libcontainerd.StateStart)
		c.HasBeenManuallyStopped = false
		if err := c.ToDisk(); err != nil {
			c.Reset(false)
			return err
		}
		daemon.LogContainerEvent(c, "start")
	case libcontainerd.StatePause:
		c.Paused = true
		if err := c.ToDisk(); err != nil {
			return err
		}
		daemon.LogContainerEvent(c, "pause")
	case libcontainerd.StateResume:
		c.Paused = false
		if err := c.ToDisk(); err != nil {
			return err
		}
		daemon.LogContainerEvent(c, "unpause")
	}

	return nil
}

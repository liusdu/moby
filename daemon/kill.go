package daemon

import (
	"fmt"
	"runtime"
	"strings"
	"syscall"
	"time"

	"github.com/Sirupsen/logrus"
	"github.com/docker/docker/container"
	"github.com/docker/docker/libcontainerd"
	"github.com/docker/docker/pkg/signal"
)

type errNoSuchProcess struct {
	pid    int
	signal int
}

func (e errNoSuchProcess) Error() string {
	return fmt.Sprintf("Cannot kill process (pid=%d) with signal %d: no such process.", e.pid, e.signal)
}

// isErrNoSuchProcess returns true if the error
// is an instance of errNoSuchProcess.
func isErrNoSuchProcess(err error) bool {
	_, ok := err.(errNoSuchProcess)
	return ok
}

// ContainerKill sends signal to the container
// If no signal is given (sig 0), then Kill with SIGKILL and wait
// for the container to exit.
// If a signal is given, then just send it to the container and return.
func (daemon *Daemon) ContainerKill(name string, sig uint64) error {
	container, err := daemon.GetContainer(name)
	if err != nil {
		return err
	}

	if sig != 0 && !signal.ValidSignalForPlatform(syscall.Signal(sig)) {
		return fmt.Errorf("The %s daemon does not support signal %d", runtime.GOOS, sig)
	}

	// If no signal is passed, or SIGKILL, perform regular Kill (SIGKILL + wait())
	if sig == 0 || syscall.Signal(sig) == syscall.SIGKILL {
		return daemon.Kill(container)
	}
	return daemon.killWithSignal(container, int(sig))
}

// killWithSignal sends the container the given signal. This wrapper for the
// host specific kill command prepares the container before attempting
// to send the signal. An error is returned if the container is paused
// or not running, or if there is a problem returned from the
// underlying kill command.
func (daemon *Daemon) killWithSignal(container *container.Container, sig int) error {
	logrus.Debugf("Sending %d to %s", sig, container.ID)
	container.Lock()
	defer container.Unlock()

	// We could unpause the container for them rather than returning this error
	if container.Paused {
		return fmt.Errorf("Container %s is paused. Unpause the container before stopping", container.ID)
	}

	if !container.Running {
		return errNotRunning{container.ID}
	}

	container.ExitOnNext()

	if !daemon.IsShuttingDown() {
		container.HasBeenManuallyStopped = true
	}

	// if the container is currently restarting we do not need to send the signal
	// to the process.  Telling the monitor that it should exit on it's next event
	// loop is enough
	if container.Restarting {
		return nil
	}

	if err := daemon.kill(container, sig); err != nil {
		err = fmt.Errorf("Cannot kill container %s: %s", container.ID, err)
		// if container or process not exists, ignore the error
		if strings.Contains(err.Error(), "container not found") ||
			strings.Contains(err.Error(), "no such process") {
			logrus.Warnf("%s", err.Error())
		} else {
			return err
		}
	}

	attributes := map[string]string{
		"signal": fmt.Sprintf("%d", sig),
	}
	daemon.LogContainerEventWithAttributes(container, "kill", attributes)
	return nil
}

// Kill forcefully terminates a container.
func (daemon *Daemon) Kill(container *container.Container) error {
	if !container.IsRunning() {
		return errNotRunning{container.ID}
	}

	// 1. Send SIGKILL
	if err := daemon.killPossiblyDeadProcess(container, int(syscall.SIGKILL)); err != nil {
		// While normally we might "return err" here we're not going to
		// because if we can't stop the container by this point then
		// its probably because its already stopped. Meaning, between
		// the time of the IsRunning() call above and now it stopped.
		// Also, since the err return will be environment specific we can't
		// look for any particular (common) error that would indicate
		// that the process is already dead vs something else going wrong.
		// So, instead we'll give it up to 2 more seconds to complete and if
		// by that time the container is still running, then the error
		// we got is probably valid and so we return it to the caller.
		if isErrNoSuchProcess(err) {
			// Here we wait 2 minutes for daemon to handle exit event
			// of this container. After this, if the container is still
			// running, maybe the exit event had lost, then we call
			// StateChanged() to change the container's state to exited.
			// If the real exit event arrives after some time, daemon
			// will call StateChanged() again, then we can see following
			// message in daemon's log:
			// ```
			// level=warning msg="error locating sandbox id <containerID>: sandbox <containerID> not found"
			// level=warning msg="failed to cleanup ipc mounts:\nfailed to umount /var/lib/docker/containers/<containerID>/shm: invalid argument"
			// level=debug msg="Failed to unmount <mountID> overlay: invalid argument"
			// ```
			container.WaitStop(120 * time.Second)
			if container.IsRunning() {
				logrus.Warnf("Failed to receive exit event of container %v after 2 minutes, manually change its status to exited", container.ID)
				return daemon.StateChanged(container.ID, libcontainerd.StateInfo{
					CommonStateInfo: libcontainerd.CommonStateInfo{
						State:    libcontainerd.StateExit,
						ExitCode: 255,
					}})
			}
			return nil
		}

		if container.IsRunning() {
			container.WaitStop(2 * time.Second)
			if container.IsRunning() {
				return err
			}
		}
	}

	// 2. Wait for the process to die, in last resort, try to kill the process directly
	if err := killProcessDirectly(container); err != nil {
		if isErrNoSuchProcess(err) {
			// Here we wait 2 minutes for daemon to handle exit event
			// of this container. After this, if the container is still
			// running, maybe the exit event had lost, then we call
			// StateChanged() to change the container's state to exited.
			// If the real exit event arrives after some time, daemon
			// will call StateChanged() again, then we can see following
			// message in daemon's log:
			// ```
			// level=warning msg="error locating sandbox id <containerID>: sandbox <containerID> not found"
			// level=warning msg="failed to cleanup ipc mounts:\nfailed to umount /var/lib/docker/containers/<containerID>/shm: invalid argument"
			// level=debug msg="Failed to unmount <mountID> overlay: invalid argument"
			// ```
			container.WaitStop(120 * time.Second)
			if container.IsRunning() {
				logrus.Warnf("Failed to receive exit event of container %v after 2 minutes, manually change its status to exited", container.ID)
				return daemon.StateChanged(container.ID, libcontainerd.StateInfo{
					CommonStateInfo: libcontainerd.CommonStateInfo{
						State:    libcontainerd.StateExit,
						ExitCode: 255,
					}})
			}
			return nil
		}
		return err
	}

	container.WaitStop(-1 * time.Second)
	return nil
}

// killPossibleDeadProcess is a wrapper around killSig() suppressing "no such process" error.
func (daemon *Daemon) killPossiblyDeadProcess(container *container.Container, sig int) error {
	err := daemon.killWithSignal(container, sig)
	if err == syscall.ESRCH {
		e := errNoSuchProcess{container.GetPID(), sig}
		logrus.Debug(e)
		return e
	}
	return err
}

func (daemon *Daemon) kill(c *container.Container, sig int) error {
	return daemon.containerd.Signal(c.ID, sig)
}

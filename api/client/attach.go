package client

import (
	"fmt"
	"io"

	"golang.org/x/net/context"

	"github.com/Sirupsen/logrus"
	Cli "github.com/docker/docker/cli"
	flag "github.com/docker/docker/pkg/mflag"
	"github.com/docker/docker/pkg/signal"
	"github.com/docker/engine-api/types"
)

// CmdAttach attaches to a running container.
//
// Usage: docker attach [OPTIONS] CONTAINER
func (cli *DockerCli) CmdAttach(args ...string) error {
	cmd := Cli.Subcmd("attach", []string{"CONTAINER"}, Cli.DockerCommands["attach"].Description, true)
	noStdin := cmd.Bool([]string{"-no-stdin"}, false, "Do not attach STDIN")
	proxy := cmd.Bool([]string{"-sig-proxy"}, true, "Proxy all received signals to the process")
	detachKeys := cmd.String([]string{"-detach-keys"}, "", "Override the key sequence for detaching a container")

	cmd.Require(flag.Exact, 1)

	cmd.ParseFlags(args, true)

	c, err := cli.getRuningContainer(cmd.Arg(0))
	if err != nil {
		return err
	}

	if err := cli.CheckTtyInput(!*noStdin, c.Config.Tty); err != nil {
		return err
	}

	if *detachKeys != "" {
		cli.configFile.DetachKeys = *detachKeys
	}

	options := types.ContainerAttachOptions{
		ContainerID: cmd.Arg(0),
		Stream:      true,
		Stdin:       !*noStdin && c.Config.OpenStdin,
		Stdout:      true,
		Stderr:      true,
		DetachKeys:  cli.configFile.DetachKeys,
	}

	var in io.ReadCloser
	if options.Stdin {
		in = cli.in
	}

	if *proxy && !c.Config.Tty {
		sigc := cli.forwardAllSignals(options.ContainerID)
		defer signal.StopCatch(sigc)
	}

	resp, err := cli.client.ContainerAttach(context.Background(), options)
	if err != nil {
		return err
	}
	defer resp.Close()

	// If use docker attach command to attach to a stop container, it will return
	// "You cannot attach to a stopped container" error, it's ok, but when
	// attach to a running container, it(docker attach) use inspect to check
	// the container's state, if it pass the state check on the client side,
	// and then the container is stopped, docker attach command still attach to
	// the container and not exit.
	//
	// Recheck the container's state again to avoid attach block.
	_, err = cli.getRuningContainer(cmd.Arg(0))
	if err != nil {
		return err
	}

	if c.Config.Tty && cli.isTerminalOut {
		height, width := cli.getTtySize()
		// To handle the case where a user repeatedly attaches/detaches without resizing their
		// terminal, the only way to get the shell prompt to display for attaches 2+ is to artificially
		// resize it, then go back to normal. Without this, every attach after the first will
		// require the user to manually resize or hit enter.
		cli.resizeTtyTo(cmd.Arg(0), height+1, width+1, false)

		// After the above resizing occurs, the call to monitorTtySize below will handle resetting back
		// to the actual size.
		if err := cli.monitorTtySize(cmd.Arg(0), false); err != nil {
			logrus.Debugf("Error monitoring TTY size: %s", err)
		}
	}
	if err := cli.holdHijackedConnection(context.Background(), c.Config.Tty, in, cli.out, cli.err, resp); err != nil {
		return err
	}

	_, status, err := getExitCode(cli, options.ContainerID)
	if err != nil {
		return err
	}
	if status != 0 {
		return Cli.StatusError{StatusCode: status}
	}

	return nil
}

func (cli *DockerCli) getRuningContainer(args string) (*types.ContainerJSON, error) {
	c, err := cli.client.ContainerInspect(context.Background(), args)
	if err != nil {
		return nil, err
	}

	if !c.State.Running {
		return nil, fmt.Errorf("You cannot attach to a stopped container, start it first")
	}

	if c.State.Paused {
		return nil, fmt.Errorf("You cannot attach to a paused container, unpause it first")
	}

	if c.State.Restarting {
		return nil, fmt.Errorf("You cannot attach to a restarting container, wait until it is running")
	}
	return &c, nil
}

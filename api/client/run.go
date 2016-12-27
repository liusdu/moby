package client

import (
	"fmt"
	"io"
	"os"
	"runtime"
	"strings"

	"golang.org/x/net/context"

	"github.com/Sirupsen/logrus"
	Cli "github.com/docker/docker/cli"
	"github.com/docker/docker/opts"
	"github.com/docker/docker/pkg/promise"
	"github.com/docker/docker/pkg/signal"
	runconfigopts "github.com/docker/docker/runconfig/opts"
	"github.com/docker/engine-api/types"
	"github.com/docker/libnetwork/resolvconf/dns"
)

const (
	errCmdNotFound          = "not found or does not exist."
	errCmdCouldNotBeInvoked = "could not be invoked."
)

func (cid *cidFile) Close() error {
	cid.file.Close()

	if !cid.written {
		if err := os.Remove(cid.path); err != nil {
			return fmt.Errorf("failed to remove the CID file '%s': %s \n", cid.path, err)
		}
	}

	return nil
}

func (cid *cidFile) Write(id string) error {
	if _, err := cid.file.Write([]byte(id)); err != nil {
		return fmt.Errorf("Failed to write the container ID to the file: %s", err)
	}
	cid.written = true
	return nil
}

// if container start fails with 'command not found' error, return 127
// if container start fails with 'command cannot be invoked' error, return 126
// return 125 for generic docker daemon failures
func runStartContainerErr(err error) error {
	trimmedErr := strings.Trim(err.Error(), "Error response from daemon: ")
	statusError := Cli.StatusError{StatusCode: 125}

	if strings.HasPrefix(trimmedErr, "Container command") {
		if strings.Contains(trimmedErr, errCmdNotFound) {
			statusError = Cli.StatusError{StatusCode: 127}
		} else if strings.Contains(trimmedErr, errCmdCouldNotBeInvoked) {
			statusError = Cli.StatusError{StatusCode: 126}
		}
	}

	return statusError
}

// CmdRun runs a command in a new container.
//
// Usage: docker run [OPTIONS] IMAGE [COMMAND] [ARG...]
func (cli *DockerCli) CmdRun(args ...string) error {
	cmd := Cli.Subcmd("run", []string{"IMAGE [COMMAND] [ARG...]"}, Cli.DockerCommands["run"].Description, true)
	addTrustedFlags(cmd, true)

	// These are flags not stored in Config/HostConfig
	var (
		flAutoRemove   = cmd.Bool([]string{"-rm"}, false, "Automatically remove the container when it exits")
		flDetach       = cmd.Bool([]string{"d", "-detach"}, false, "Run container in background and print container ID")
		flSigProxy     = cmd.Bool([]string{"-sig-proxy"}, true, "Proxy received signals to the process")
		flName         = cmd.String([]string{"-name"}, "", "Assign a name to the container")
		flDetachKeys   = cmd.String([]string{"-detach-keys"}, "", "Override the key sequence for detaching a container")
		flAttachOutput = cmd.String([]string{"-attach-output"}, "", "Attach the output stream of a container")
		flAttach       *opts.ListOpts

		ErrConflictAttachDetach               = fmt.Errorf("Conflicting options: -a and -d")
		ErrConflictAttachOutputDetach         = fmt.Errorf("Conflicting options: --attach-output and -d")
		ErrConflictRestartPolicyAndAutoRemove = fmt.Errorf("Conflicting options: --restart and --rm")
		ErrConflictDetachAutoRemove           = fmt.Errorf("Conflicting options: --rm and -d")
	)

	config, hostConfig, networkingConfig, cmd, err := runconfigopts.Parse(cmd, args)

	// just in case the Parse does not exit
	if err != nil {
		cmd.ReportError(err.Error(), true)
		os.Exit(125)
	}

	if hostConfig.OomKillDisable != nil && *hostConfig.OomKillDisable && hostConfig.Memory == 0 {
		fmt.Fprintf(cli.err, "WARNING: Disabling the OOM killer on containers without setting a '-m/--memory' limit may be dangerous.\n")
	}

	if len(hostConfig.DNS) > 0 {
		// check the DNS settings passed via --dns against
		// localhost regexp to warn if they are trying to
		// set a DNS to a localhost address
		for _, dnsIP := range hostConfig.DNS {
			if dns.IsLocalhost(dnsIP) {
				fmt.Fprintf(cli.err, "WARNING: Localhost DNS setting (--dns=%s) may fail in containers.\n", dnsIP)
				break
			}
		}
	}
	if config.Image == "" {
		cmd.Usage()
		return nil
	}

	config.ArgsEscaped = false

	if !*flDetach {
		if err := cli.CheckTtyInput(config.AttachStdin, config.Tty); err != nil {
			return err
		}
	} else {
		if fl := cmd.Lookup("-attach"); fl != nil {
			flAttach = fl.Value.(*opts.ListOpts)
			if flAttach.Len() != 0 {
				return ErrConflictAttachDetach
			}
		}
		if *flAutoRemove {
			return ErrConflictDetachAutoRemove
		}

		// Technically `--attach-output` can live with `-d`, because it
		// attaches another container's output, but we don't want it be
		// complicated or confusing because people usually don't want
		// we mess up with the stdout when they want to be detached.
		if *flAttachOutput != "" {
			return ErrConflictAttachOutputDetach
		}

		config.AttachStdin = false
		config.AttachStdout = false
		config.AttachStderr = false
		config.StdinOnce = false
	}

	// Disable flSigProxy when in TTY mode
	sigProxy := *flSigProxy
	if config.Tty {
		sigProxy = false
	}

	// Telling the Windows daemon the initial size of the tty during start makes
	// a far better user experience rather than relying on subsequent resizes
	// to cause things to catch up.
	if runtime.GOOS == "windows" {
		hostConfig.ConsoleSize[0], hostConfig.ConsoleSize[1] = cli.getTtySize()
	}

	createResponse, err := cli.createContainer(config, hostConfig, networkingConfig, hostConfig.ContainerIDFile, *flName)
	if err != nil {
		cmd.ReportError(err.Error(), true)
		return runStartContainerErr(err)
	}
	if sigProxy {
		sigc := cli.forwardAllSignals(createResponse.ID)
		defer signal.StopCatch(sigc)
	}
	var (
		waitDisplayID chan struct{}
		errCh         chan error
		errCh2        chan error
		cancelFun     context.CancelFunc
		ctx           context.Context
	)
	if !config.AttachStdout && !config.AttachStderr {
		// Make this asynchronous to allow the client to write to stdin before having to read the ID
		waitDisplayID = make(chan struct{})
		go func() {
			defer close(waitDisplayID)
			fmt.Fprintf(cli.out, "%s\n", createResponse.ID)
		}()
	}
	if *flAutoRemove && (hostConfig.RestartPolicy.IsAlways() || hostConfig.RestartPolicy.IsOnFailure() || hostConfig.RestartPolicy.IsOnReboot()) {
		return ErrConflictRestartPolicyAndAutoRemove
	}
	attach := config.AttachStdin || config.AttachStdout || config.AttachStderr
	if attach {
		var (
			out, stderr io.Writer
			in          io.ReadCloser
		)
		if config.AttachStdin {
			in = cli.in
		}
		if config.AttachStdout {
			out = cli.out
		}
		if config.AttachStderr {
			if config.Tty {
				stderr = cli.out
			} else {
				stderr = cli.err
			}
		}

		if *flDetachKeys != "" {
			cli.configFile.DetachKeys = *flDetachKeys
		}

		options := types.ContainerAttachOptions{
			ContainerID: createResponse.ID,
			Stream:      true,
			Stdin:       config.AttachStdin,
			Stdout:      config.AttachStdout,
			Stderr:      config.AttachStderr,
			DetachKeys:  cli.configFile.DetachKeys,
		}

		resp, err := cli.client.ContainerAttach(context.Background(), options)
		if err != nil {
			return err
		}
		ctx, cancelFun = context.WithCancel(context.Background())
		errCh = promise.Go(func() error {
			return cli.holdHijackedConnection(ctx, config.Tty, in, out, stderr, resp)
		})

		if *flAttachOutput != "" {
			c, err := cli.client.ContainerInspect(context.Background(), *flAttachOutput)
			if err != nil {
				return err
			}
			options2 := types.ContainerAttachOptions{
				ContainerID: c.ID,
				Stream:      true,
				Stdin:       false,
				Stdout:      true,
				Stderr:      true,
				DetachKeys:  cli.configFile.DetachKeys,
			}
			resp2, err := cli.client.ContainerAttach(context.Background(), options2)
			if err != nil {
				return err
			}
			errCh2 = promise.Go(func() error {
				return cli.holdHijackedConnection(ctx, config.Tty, nil, out, stderr, resp2)
			})
			defer func() {
				resp2.Close()
				if errCh2 != nil {
					if err := <-errCh2; err != nil {
						logrus.Debugf("Error hijack2: %s", err)
					}
				}
			}()
		}

	} else {
		if *flAttachOutput != "" {
			return fmt.Errorf("Can not attach a container's output without IO streams enabled.")
		}
	}

	if *flAutoRemove {
		defer func() {
			if err := cli.removeContainer(createResponse.ID, true, false, false); err != nil {
				fmt.Fprintf(cli.err, "%v\n", err)
			}
		}()
	}

	//start the container
	if err := cli.client.ContainerStart(context.Background(), createResponse.ID); err != nil {
		// If we have holdHijackedConnection, we should notify
		// holdHijackedConnection we are going to exit and wait
		// to avoid the terminal are not restored.
		if attach {
			cancelFun()
			<-errCh
		}

		cmd.ReportError(err.Error(), false)
		return runStartContainerErr(err)
	}

	if (config.AttachStdin || config.AttachStdout || config.AttachStderr) && config.Tty && cli.isTerminalOut {
		if err := cli.monitorTtySize(createResponse.ID, false); err != nil {
			fmt.Fprintf(cli.err, "Error monitoring TTY size: %s\n", err)
		}
	}

	if errCh != nil {
		if err := <-errCh; err != nil {
			logrus.Debugf("Error hijack: %s", err)
			return err
		}
	}

	// Detached mode: wait for the id to be displayed and return.
	if !config.AttachStdout && !config.AttachStderr {
		// Detached mode
		<-waitDisplayID
		return nil
	}

	var status int

	// Attached mode
	if *flAutoRemove {
		// Autoremove: wait for the container to finish, retrieve
		// the exit code and remove the container
		if status, err = cli.client.ContainerWait(context.Background(), createResponse.ID); err != nil {
			return runStartContainerErr(err)
		}
		if _, status, err = getExitCode(cli, createResponse.ID); err != nil {
			return err
		}
	} else {
		// No Autoremove: Simply retrieve the exit code
		if !config.Tty {
			// In non-TTY mode, we can't detach, so we must wait for container exit
			if status, err = cli.client.ContainerWait(context.Background(), createResponse.ID); err != nil {
				return err
			}
		} else {
			// In TTY mode, there is a race: if the process dies too slowly, the state could
			// be updated after the getExitCode call and result in the wrong exit code being reported
			if _, status, err = getExitCode(cli, createResponse.ID); err != nil {
				return err
			}
		}
	}
	if status != 0 {
		return Cli.StatusError{StatusCode: status}
	}
	return nil
}

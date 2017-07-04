package daemon

import (
	"fmt"
	"io"
	"strconv"
	"time"

	"github.com/Sirupsen/logrus"
	"github.com/docker/docker/api/types/backend"
	"github.com/docker/docker/container"
	"github.com/docker/docker/daemon/logger"
	"github.com/docker/docker/pkg/ioutils"
	"github.com/docker/docker/pkg/stdcopy"
	containertypes "github.com/docker/engine-api/types/container"
	timetypes "github.com/docker/engine-api/types/time"
)

// ContainerLogs hooks up a container's stdout and stderr streams
// configured with the given struct.
func (daemon *Daemon) ContainerLogs(containerName string, config *backend.ContainerLogsConfig, started chan struct{}) error {
	container, err := daemon.GetContainer(containerName)
	if err != nil {
		return err
	}

	if container.RemovalInProgress || container.Dead {
		return fmt.Errorf("Can not get logs from container which is dead or marked for removal")
	}

	if !(config.ShowStdout || config.ShowStderr) {
		return fmt.Errorf("You must choose at least one stream")
	}

	cLog, cLogCreated, err := daemon.getLogger(container)
	if err != nil {
		return err
	}
	if cLogCreated {
		defer func() {
			if err = cLog.Close(); err != nil {
				logrus.Errorf("Error closing logger: %v", err)
			}
		}()
	}

	logReader, ok := cLog.(logger.LogReader)
	if !ok {
		return logger.ErrReadLogsNotSupported
	}

	follow := config.Follow && !cLogCreated
	tailLines, err := strconv.Atoi(config.Tail)
	if err != nil {
		tailLines = -1
	}

	logrus.Debug("logs: begin stream")

	var since time.Time
	if config.Since != "" {
		s, n, err := timetypes.ParseTimestamps(config.Since, 0)
		if err != nil {
			return err
		}
		since = time.Unix(s, n)
	}
	readConfig := logger.ReadConfig{
		Since:  since,
		Tail:   tailLines,
		Follow: follow,
	}
	logs := logReader.ReadLogs(readConfig)
	defer logs.Close()

	wf := ioutils.NewWriteFlusher(config.OutStream)
	defer wf.Close()
	close(started)
	wf.Flush()

	var outStream io.Writer = wf
	errStream := outStream
	if !container.Config.Tty {
		errStream = stdcopy.NewStdWriter(outStream, stdcopy.Stderr)
		outStream = stdcopy.NewStdWriter(outStream, stdcopy.Stdout)
	}

	for {
		select {
		case err := <-logs.Err:
			logrus.Errorf("Error streaming logs: %v", err)
			return nil
		case <-config.Stop:
			logrus.Debugf("logs: end stream, ctx is done")
			return nil
		case msg, ok := <-logs.Msg:
			if !ok {
				logrus.Debugf("logs: end stream")
				return nil
			}
			logLine := msg.Line
			if config.Timestamps {
				logLine = append([]byte(msg.Timestamp.Format(logger.TimeFormat)+" "), logLine...)
			}
			if msg.Source == "stdout" && config.ShowStdout {
				outStream.Write(logLine)
			}
			if msg.Source == "stderr" && config.ShowStderr {
				errStream.Write(logLine)
			}
		}
	}
}

func (daemon *Daemon) getLogger(container *container.Container) (l logger.Logger, created bool, err error) {
	container.Lock()
	if container.State.Running {
		l = container.LogDriver
	}
	container.Unlock()
	if l == nil {
		created = true
		l, err = container.StartLogger(container.HostConfig.LogConfig)
	}
	return
}

// mergeLogConfig merges the daemon log config to the container's log config if the container's log driver is not specified.
func (daemon *Daemon) mergeAndVerifyLogConfig(cfg *containertypes.LogConfig) error {
	if cfg.Type == "" {
		cfg.Type = daemon.defaultLogConfig.Type
	}

	if cfg.Config == nil {
		cfg.Config = make(map[string]string)
	}

	if cfg.Type == daemon.defaultLogConfig.Type {
		for k, v := range daemon.defaultLogConfig.Config {
			if _, ok := cfg.Config[k]; !ok {
				cfg.Config[k] = v
			}
		}
	}

	return logger.ValidateLogOpts(cfg.Type, cfg.Config)
}

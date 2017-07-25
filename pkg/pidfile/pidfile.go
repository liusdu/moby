// Package pidfile provides structure and helper functions to create and remove
// PID file. A PID file is usually a file used to store the process ID of a
// running process.
package pidfile

import (
	"bufio"
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// PIDFile is a file used to store the process ID of a running process.
type PIDFile struct {
	path string
}

// isSameApplication check whether the pid exist in pidfile
// is the the same application we are going to run.
func isSameApplication(pid int) (bool, error) {
	path := filepath.Join("/proc", strconv.Itoa(pid), "status")
	file, err := os.Open(path)
	if err != nil {
		return false, err
	}
	defer file.Close()
	sc := bufio.NewScanner(file)
	for sc.Scan() {
		lens := strings.Split(sc.Text(), ":")
		if len(lens) == 2 && strings.TrimSpace(lens[0]) == "Name" {
			if strings.TrimSpace(lens[1]) == filepath.Base(os.Args[0]) {
				return true, nil
			}
			return false, nil
		}
	}
	if err := sc.Err(); err != nil {
		return false, err
	}
	return false, nil
}

func checkPIDFileAlreadyExists(path string) error {
	if pidByte, err := ioutil.ReadFile(path); err == nil {
		pidString := strings.TrimSpace(string(pidByte))
		if pid, err := strconv.Atoi(pidString); err == nil {
			if _, err := os.Stat(filepath.Join("/proc", strconv.Itoa(pid))); err == nil {
				if same, err := isSameApplication(pid); same || (err != nil && !os.IsNotExist(err)) {
					return fmt.Errorf("pid file found, ensure docker is not running or delete %s", path)
				}
			}
		}
	}
	return nil
}

// New creates a PIDfile using the specified path.
func New(path string) (*PIDFile, error) {
	if err := checkPIDFileAlreadyExists(path); err != nil {
		return nil, err
	}
	if err := ioutil.WriteFile(path, []byte(fmt.Sprintf("%d", os.Getpid())), 0644); err != nil {
		return nil, err
	}

	return &PIDFile{path: path}, nil
}

// Remove removes the PIDFile.
func (file PIDFile) Remove() error {
	if err := os.Remove(file.path); err != nil {
		return err
	}
	return nil
}

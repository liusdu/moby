// +build linux

package sysinfo

import (
	"bufio"
	"fmt"
	"os"
	"strings"

	"github.com/docker/go-units"
	"github.com/opencontainers/runc/libcontainer/cgroups"
)

// GetHugepageSize returns system supported hugepage sizes
func GetHugepageSize() (string, error) {
	hps, err := getHugepageSizes()
	if err != nil {
		return "", err
	}

	dhp, err := GetDefaultHugepageSize()
	if err != nil {
		return "", err
	}

	hpsString := strings.Join(hps, ", ")
	if len(hps) > 1 {
		hpsString += fmt.Sprintf(" (default is %s)", dhp)
	}
	return hpsString, nil
}

// ValidateHugetlb check whether hugetlb pagesize and limit legal
func ValidateHugetlb(pageSize string, limit uint64) (string, []string, error) {
	var err error
	warnings := []string{}
	if pageSize != "" {
		sizeInt, _ := units.RAMInBytes(pageSize)
		pageSize = humanSize(sizeInt)
		if err := isHugepageSizeValid(pageSize); err != nil {
			return "", warnings, err
		}
	} else {
		pageSize, err = GetDefaultHugepageSize()
		if err != nil {
			return "", warnings, fmt.Errorf("Failed to get system hugepage size")
		}
	}

	warn, err := isHugeLimitValid(pageSize, limit)
	warnings = append(warnings, warn...)
	if err != nil {
		return "", warnings, err
	}

	return pageSize, warnings, nil
}

// isHugeLimitValid check whether input hugetlb limit legal
// it will check whether the limit size is times of size
func isHugeLimitValid(size string, limit uint64) ([]string, error) {
	warnings := []string{}
	sizeInt, err := units.RAMInBytes(size)
	if err != nil || sizeInt < 0 {
		return warnings, fmt.Errorf("Invalid hugepage size:%s -- %s", size, err)
	}
	sizeUint := uint64(sizeInt)

	if limit%sizeUint != 0 {
		warnings = append(warnings, "HugeTlb limit should be times of hugepage size. "+
			"cgroup will down round to the nearest multiple")
	}

	return warnings, nil
}

// isHugepageSizeValid check whether input size legal
// it will compare size with all system supported hugepage size
func isHugepageSizeValid(size string) error {
	hps, err := getHugepageSizes()
	if err != nil {
		return err
	}

	for _, hp := range hps {
		if size == hp {
			return nil
		}
	}
	return fmt.Errorf("Invalid hugepage size:%s, shoud be one of %v", size, hps)
}

func humanSize(i int64) string {
	// hugetlb may not surpass GB
	uf := []string{"B", "KB", "MB", "GB"}
	ui := 0
	for {
		if i < 1024 || ui >= 3 {
			break
		}
		i = int64(i / 1024)
		ui = ui + 1
	}

	return fmt.Sprintf("%d%s", i, uf[ui])
}

func getHugepageSizes() ([]string, error) {
	var hps []string

	hgtlbMp, err := cgroups.FindCgroupMountpoint("hugetlb")
	if err != nil {
		return nil, fmt.Errorf("Hugetlb cgroup not supported")
	}

	f, err := os.Open(hgtlbMp)
	if err != nil {
		return nil, fmt.Errorf("Failed to open hugetlb cgroup directory")
	}
	// -1 here means to read all the fileInfo from the directory, could be any negative number
	fi, err := f.Readdir(-1)
	if err != nil {
		return nil, fmt.Errorf("Failed to read hugetlb cgroup directory")
	}

	for _, finfo := range fi {
		if strings.Contains(finfo.Name(), "limit_in_bytes") {
			sres := strings.SplitN(finfo.Name(), ".", 3)
			if len(sres) != 3 {
				continue
			}
			hps = append(hps, sres[1])
		}
	}

	if len(hps) == 0 {
		return nil, fmt.Errorf("Hugetlb pagesize not found in cgroup")
	}

	return hps, nil
}

// GetDefaultHugepageSize returns system default hugepage size
func GetDefaultHugepageSize() (string, error) {
	f, err := os.Open("/proc/meminfo")
	if err != nil {
		return "", fmt.Errorf("Failed to get hugepage size, cannot open /proc/meminfo")
	}
	defer f.Close()

	s := bufio.NewScanner(f)
	for s.Scan() {
		if strings.Contains(s.Text(), "Hugepagesize") {
			sres := strings.SplitN(s.Text(), ":", 2)
			if len(sres) != 2 {
				return "", fmt.Errorf("Failed to get hugepage size, weird /proc/meminfo format")
			}

			// return strings.TrimSpace(sres[1]), nil
			size := strings.Replace(sres[1], " ", "", -1)
			// transform 2048k to 2M
			sizeInt, _ := units.RAMInBytes(size)
			return humanSize(sizeInt), nil
		}
	}
	return "", fmt.Errorf("Failed to get hugepage size")
}

// +build !windows

package daemon

import (
	"io/ioutil"
	"os"
	"testing"

	"github.com/docker/docker/container"
	containertypes "github.com/docker/engine-api/types/container"
)

// Unix test as uses settings which are not available on Windows
func TestAdjustCPUShares(t *testing.T) {
	tmp, err := ioutil.TempDir("", "docker-daemon-unix-test-")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmp)
	daemon := &Daemon{
		repository: tmp,
		root:       tmp,
	}

	hostConfig := &containertypes.HostConfig{
		Resources: containertypes.Resources{CPUShares: linuxMinCPUShares - 1},
	}
	daemon.adaptContainerSettings(hostConfig, true)
	if hostConfig.CPUShares != linuxMinCPUShares {
		t.Errorf("Expected CPUShares to be %d", linuxMinCPUShares)
	}

	hostConfig.CPUShares = linuxMaxCPUShares + 1
	daemon.adaptContainerSettings(hostConfig, true)
	if hostConfig.CPUShares != linuxMaxCPUShares {
		t.Errorf("Expected CPUShares to be %d", linuxMaxCPUShares)
	}

	hostConfig.CPUShares = 0
	daemon.adaptContainerSettings(hostConfig, true)
	if hostConfig.CPUShares != 0 {
		t.Error("Expected CPUShares to be unchanged")
	}

	hostConfig.CPUShares = 1024
	daemon.adaptContainerSettings(hostConfig, true)
	if hostConfig.CPUShares != 1024 {
		t.Error("Expected CPUShares to be unchanged")
	}
}

// Unix test as uses settings which are not available on Windows
func TestAdjustCPUSharesNoAdjustment(t *testing.T) {
	tmp, err := ioutil.TempDir("", "docker-daemon-unix-test-")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmp)
	daemon := &Daemon{
		repository: tmp,
		root:       tmp,
	}

	hostConfig := &containertypes.HostConfig{
		Resources: containertypes.Resources{CPUShares: linuxMinCPUShares - 1},
	}
	daemon.adaptContainerSettings(hostConfig, false)
	if hostConfig.CPUShares != linuxMinCPUShares-1 {
		t.Errorf("Expected CPUShares to be %d", linuxMinCPUShares-1)
	}

	hostConfig.CPUShares = linuxMaxCPUShares + 1
	daemon.adaptContainerSettings(hostConfig, false)
	if hostConfig.CPUShares != linuxMaxCPUShares+1 {
		t.Errorf("Expected CPUShares to be %d", linuxMaxCPUShares+1)
	}

	hostConfig.CPUShares = 0
	daemon.adaptContainerSettings(hostConfig, false)
	if hostConfig.CPUShares != 0 {
		t.Error("Expected CPUShares to be unchanged")
	}

	hostConfig.CPUShares = 1024
	daemon.adaptContainerSettings(hostConfig, false)
	if hostConfig.CPUShares != 1024 {
		t.Error("Expected CPUShares to be unchanged")
	}
}

// Unix test as uses settings which are not available on Windows
func TestParseSecurityOptWithDeprecatedColon(t *testing.T) {
	container := &container.Container{}
	config := &containertypes.HostConfig{}

	// test apparmor
	config.SecurityOpt = []string{"apparmor=test_profile"}
	if err := parseSecurityOpt(container, config); err != nil {
		t.Fatalf("Unexpected parseSecurityOpt error: %v", err)
	}
	if container.AppArmorProfile != "test_profile" {
		t.Fatalf("Unexpected AppArmorProfile, expected: \"test_profile\", got %q", container.AppArmorProfile)
	}

	// test seccomp
	sp := "/path/to/seccomp_test.json"
	config.SecurityOpt = []string{"seccomp=" + sp}
	if err := parseSecurityOpt(container, config); err != nil {
		t.Fatalf("Unexpected parseSecurityOpt error: %v", err)
	}
	if container.SeccompProfile != sp {
		t.Fatalf("Unexpected AppArmorProfile, expected: %q, got %q", sp, container.SeccompProfile)
	}

	// test valid label
	config.SecurityOpt = []string{"label=user:USER"}
	if err := parseSecurityOpt(container, config); err != nil {
		t.Fatalf("Unexpected parseSecurityOpt error: %v", err)
	}

	// test invalid label
	config.SecurityOpt = []string{"label"}
	if err := parseSecurityOpt(container, config); err == nil {
		t.Fatal("Expected parseSecurityOpt error, got nil")
	}

	// test invalid opt
	config.SecurityOpt = []string{"test"}
	if err := parseSecurityOpt(container, config); err == nil {
		t.Fatal("Expected parseSecurityOpt error, got nil")
	}
}

func TestParseSecurityOpt(t *testing.T) {
	container := &container.Container{}
	config := &containertypes.HostConfig{}

	// test apparmor
	config.SecurityOpt = []string{"apparmor=test_profile"}
	if err := parseSecurityOpt(container, config); err != nil {
		t.Fatalf("Unexpected parseSecurityOpt error: %v", err)
	}
	if container.AppArmorProfile != "test_profile" {
		t.Fatalf("Unexpected AppArmorProfile, expected: \"test_profile\", got %q", container.AppArmorProfile)
	}

	// test seccomp
	sp := "/path/to/seccomp_test.json"
	config.SecurityOpt = []string{"seccomp=" + sp}
	if err := parseSecurityOpt(container, config); err != nil {
		t.Fatalf("Unexpected parseSecurityOpt error: %v", err)
	}
	if container.SeccompProfile != sp {
		t.Fatalf("Unexpected SeccompProfile, expected: %q, got %q", sp, container.SeccompProfile)
	}

	// test valid label
	config.SecurityOpt = []string{"label=user:USER"}
	if err := parseSecurityOpt(container, config); err != nil {
		t.Fatalf("Unexpected parseSecurityOpt error: %v", err)
	}

	// test invalid label
	config.SecurityOpt = []string{"label"}
	if err := parseSecurityOpt(container, config); err == nil {
		t.Fatal("Expected parseSecurityOpt error, got nil")
	}

	// test invalid opt
	config.SecurityOpt = []string{"test"}
	if err := parseSecurityOpt(container, config); err == nil {
		t.Fatal("Expected parseSecurityOpt error, got nil")
	}
}

func TestNetworkOptions(t *testing.T) {
	daemon := &Daemon{}
	dconfigCorrect := &Config{
		CommonConfig: CommonConfig{
			ClusterStore:     "consul://localhost:8500",
			ClusterAdvertise: "192.168.0.1:8000",
		},
	}

	if _, err := daemon.networkOptions(dconfigCorrect, nil); err != nil {
		t.Fatalf("Expect networkOptions success, got error: %v", err)
	}

	dconfigWrong := &Config{
		CommonConfig: CommonConfig{
			ClusterStore: "consul://localhost:8500://test://bbb",
		},
	}

	if _, err := daemon.networkOptions(dconfigWrong, nil); err == nil {
		t.Fatalf("Expected networkOptions error, got nil")
	}
}

func TestGetContainerMountId(t *testing.T) {
	id := "56e143922c405419a38b23bfbccc92284f35525e3f2ad7011ea904501ccd1219"

	isInitdev, id1 := getContainerMountId("/var/lib/docker/aufs/mnt/" + id)
	if id1 != id {
		t.Fatalf("Expected container mount id [%s], but got [%s]", id, id1)
	} else if isInitdev {
		t.Fatalf("Expected the mounted device is not a init device")
	}

	isInitdev, id1 = getContainerMountId("/var/lib/docker/devicemapper/mnt/" + id)
	if id1 != id {
		t.Fatalf("Expected container mount id [%s], but got [%s]", id, id1)
	} else if isInitdev {
		t.Fatalf("Expected the mounted device is not a init device")
	}

	isInitdev, id1 = getContainerMountId("/var/lib/docker/devicemapper/mnt/" + id + "-init")
	if id1 != id {
		t.Fatalf("Expected container mount id [%s], but got [%s]", id, id1)
	} else if isInitdev == false {
		t.Fatalf("Expected the mounted device to be a init device")
	}

	isInitdev, id1 = getContainerMountId("/var/lib/docker/overlay/" + id + "/merged")
	if id1 != id {
		t.Fatalf("Expected container mount id [%s], but got [%s]", id, id1)
	} else if isInitdev {
		t.Fatalf("Expected the mounted device is not a init device")
	}

	isInitdev, id1 = getContainerMountId("/var/lib/docker/zfs/graph/" + id)
	if id1 != id {
		t.Fatalf("Expected container mount id [%s], but got [%s]", id, id1)
	} else if isInitdev {
		t.Fatalf("Expected the mounted device is not a init device")
	}

	isInitdev, id1 = getContainerMountId("/var/lib/docker/devicemapper_err/mnt" + id)
	if id1 != "" {
		t.Fatalf("Expected a empty container mount id, but got [%s]", id1)
	} else if isInitdev {
		t.Fatalf("Expected the mounted device is not a init device")
	}

	isInitdev, id1 = getContainerMountId("/var/lib/docker/devicemapper_err/mnt" + id + "-init")
	if id1 != "" {
		t.Fatalf("Expected a empty container mount id, but got [%s]", id1)
	} else if isInitdev {
		t.Fatalf("Expected the mounted device is not a init device")
	}
}

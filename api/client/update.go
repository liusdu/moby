package client

import (
	"fmt"
	"strings"

	"golang.org/x/net/context"

	Cli "github.com/docker/docker/cli"
	cliOpts "github.com/docker/docker/opts"
	flag "github.com/docker/docker/pkg/mflag"
	"github.com/docker/docker/runconfig/opts"
	"github.com/docker/engine-api/types/container"
	"github.com/docker/go-units"
)

func parseDeviceOpts(mp cliOpts.ListOpts) (map[string]container.DeviceMapping, error) {
	array := make(map[string]container.DeviceMapping)
	for dev := range mp.GetMap() {
		device, err := opts.ParseDevice(dev)
		if err != nil {
			return nil, err
		}
		array[device.PathInContainer] = device
	}
	return array, nil
}

func parseBindsOpts(mp cliOpts.ListOpts) (map[string]string, error) {
	binds := make(map[string]string)
	for bind := range mp.GetMap() {
		arr := opts.VolumeSplitN(bind, 2)
		if len(arr) <= 1 {
			return nil, fmt.Errorf("Binds lack of ':' for bind,(%s)", bind)
		}
		binds[arr[1]] = bind
	}
	return binds, nil
}

func mergeDevices(origDevices []container.DeviceMapping, deviceToAdd, deviceToRm map[string]container.DeviceMapping) ([]container.DeviceMapping, error) {
	var finalDevices []container.DeviceMapping

	// Delete devices from origDevices
	for _, oDev := range origDevices {
		needKeep := true
		for key, dev := range deviceToRm {
			if dev.PathOnHost == oDev.PathOnHost && dev.PathInContainer == oDev.PathInContainer {
				needKeep = false
				delete(deviceToRm, key)
				break
			}
		}
		if needKeep {
			finalDevices = append(finalDevices, oDev)
		}
	}
	if len(deviceToRm) > 0 {
		return nil, fmt.Errorf("Can not remove non-existing device: %v", deviceToRm)
	}

	// Add new devices here
	for _, dev := range deviceToAdd {
		for _, eDevice := range origDevices {
			if dev.PathOnHost == eDevice.PathOnHost && dev.PathInContainer == eDevice.PathInContainer {
				return nil, fmt.Errorf("Can not add existing device: %v", dev)
			}
		}
		finalDevices = append(finalDevices, dev)
	}
	return finalDevices, nil
}

func mergeBinds(origBinds []string, bindsAdd, bindsRm map[string]string) ([]string, error) {
	var finalBinds []string

	// Remove binds from origBinds
	for _, eBind := range origBinds {
		bindExist := false
		eBindArr := strings.Split(eBind, ":")
		for key, rmbind := range bindsRm {
			// After parsing binds, we are sure that bind has ":"
			rmBindArr := strings.Split(rmbind, ":")
			if rmBindArr[0] == eBindArr[0] && rmBindArr[1] == eBindArr[1] {
				bindExist = true
				delete(bindsRm, key)
				break
			}
		}
		// Ignore eBind, it should be remove.
		if bindExist {
			continue
		}
		finalBinds = append(finalBinds, eBind)
	}
	if len(bindsRm) > 0 {
		return nil, fmt.Errorf("Can not remove non-existing binds: %v", bindsRm)
	}

	// Add binds to finalBinds
	for _, addBind := range bindsAdd {
		bindArr := strings.Split(addBind, ":")
		for _, eBind := range origBinds {
			eBindArr := strings.Split(eBind, ":")
			if bindArr[0] == eBindArr[0] && bindArr[1] == eBindArr[1] {
				return nil, fmt.Errorf("Can not add existing binds: %v", eBind)
			}
		}
		finalBinds = append(finalBinds, addBind)
	}
	return finalBinds, nil
}

// CmdUpdate updates resources of one or more containers.
//
// Usage: docker update [OPTIONS] CONTAINER [CONTAINER...]
func (cli *DockerCli) CmdUpdate(args ...string) error {
	flAddDevice := cliOpts.NewListOpts(opts.ValidateDevice)
	flRmDevice := cliOpts.NewListOpts(opts.ValidateDevice)
	flAddBinds := cliOpts.NewListOpts(nil)
	flRmBinds := cliOpts.NewListOpts(nil)

	cmd := Cli.Subcmd("update", []string{"CONTAINER [CONTAINER...]"}, Cli.DockerCommands["update"].Description, true)
	flBlkioWeight := cmd.Uint16([]string{"-blkio-weight"}, 0, "Block IO (relative weight), between 10 and 1000")
	flCPUPeriod := cmd.Int64([]string{"-cpu-period"}, 0, "Limit CPU CFS (Completely Fair Scheduler) period")
	flCPUQuota := cmd.Int64([]string{"-cpu-quota"}, 0, "Limit CPU CFS (Completely Fair Scheduler) quota")
	flCpusetCpus := cmd.String([]string{"-cpuset-cpus"}, "", "CPUs in which to allow execution (0-3, 0,1)")
	flCpusetMems := cmd.String([]string{"-cpuset-mems"}, "", "MEMs in which to allow execution (0-3, 0,1)")
	flCPUShares := cmd.Int64([]string{"#c", "-cpu-shares"}, 0, "CPU shares (relative weight)")
	flMemoryString := cmd.String([]string{"m", "-memory"}, "", "Memory limit")
	flMemoryReservation := cmd.String([]string{"-memory-reservation"}, "", "Memory soft limit")
	flMemorySwap := cmd.String([]string{"-memory-swap"}, "", "Swap limit equal to memory plus swap: '-1' to enable unlimited swap")
	flKernelMemory := cmd.String([]string{"-kernel-memory"}, "", "Kernel memory limit")
	flRestartPolicy := cmd.String([]string{"-restart"}, "", "Restart policy to apply when a container exits")
	cmd.Var(&flAddDevice, []string{"-add-device"}, "Add device to container")
	cmd.Var(&flRmDevice, []string{"-remove-device"}, "Remove device from container")
	cmd.Var(&flAddBinds, []string{"-add-path"}, "Add host path to container")
	cmd.Var(&flRmBinds, []string{"-remove-path"}, "Remove host path from container")

	cmd.Require(flag.Min, 1)
	cmd.ParseFlags(args, true)
	if cmd.NFlag() == 0 {
		return fmt.Errorf("You must provide one or more flags when using this command.")
	}

	var err error
	var flMemory int64
	if *flMemoryString != "" {
		flMemory, err = units.RAMInBytes(*flMemoryString)
		if err != nil {
			return err
		}
	}

	var memoryReservation int64
	if *flMemoryReservation != "" {
		memoryReservation, err = units.RAMInBytes(*flMemoryReservation)
		if err != nil {
			return err
		}
	}

	var memorySwap int64
	if *flMemorySwap != "" {
		if *flMemorySwap == "-1" {
			memorySwap = -1
		} else {
			memorySwap, err = units.RAMInBytes(*flMemorySwap)
			if err != nil {
				return err
			}
		}
	}

	var kernelMemory int64
	if *flKernelMemory != "" {
		kernelMemory, err = units.RAMInBytes(*flKernelMemory)
		if err != nil {
			return err
		}
	}

	var restartPolicy container.RestartPolicy
	if *flRestartPolicy != "" {
		restartPolicy, err = opts.ParseRestartPolicy(*flRestartPolicy)
		if err != nil {
			return err
		}
	}

	devicesToAdd, err := parseDeviceOpts(flAddDevice)
	if err != nil {
		return err
	}
	devicesToRm, err := parseDeviceOpts(flRmDevice)
	if err != nil {
		return err
	}
	bindsToAdd, err := parseBindsOpts(flAddBinds)
	if err != nil {
		return err
	}
	bindsToRm, err := parseBindsOpts(flRmBinds)
	if err != nil {
		return err
	}

	resources := container.Resources{
		BlkioWeight:       *flBlkioWeight,
		CpusetCpus:        *flCpusetCpus,
		CpusetMems:        *flCpusetMems,
		CPUShares:         *flCPUShares,
		Memory:            flMemory,
		MemoryReservation: memoryReservation,
		MemorySwap:        memorySwap,
		KernelMemory:      kernelMemory,
		CPUPeriod:         *flCPUPeriod,
		CPUQuota:          *flCPUQuota,
	}

	updateConfig := container.UpdateConfig{
		Resources:     resources,
		RestartPolicy: restartPolicy,
	}

	names := cmd.Args()
	var errs []string
	for _, name := range names {
		containerJson, err := cli.client.ContainerInspect(context.Background(), name)
		if err != nil {
			return err
		}
		//update container devices.
		origDevices := containerJson.ContainerJSONBase.HostConfig.Resources.Devices
		devices, err := mergeDevices(origDevices, devicesToAdd, devicesToRm)
		if err != nil {
			return err
		}
		updateConfig.Resources.Devices = devices

		//update container binds.
		origBinds := containerJson.ContainerJSONBase.HostConfig.Binds
		binds, err := mergeBinds(origBinds, bindsToAdd, bindsToRm)
		if err != nil {
			return err
		}
		updateConfig.Binds = binds

		if err := cli.client.ContainerUpdate(context.Background(), name, updateConfig); err != nil {
			errs = append(errs, err.Error())
		} else {
			fmt.Fprintf(cli.out, "%s\n", name)
		}
	}

	if len(errs) > 0 {
		return fmt.Errorf("%s", strings.Join(errs, "\n"))
	}

	return nil
}

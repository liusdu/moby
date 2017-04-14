// +build accdrv

package fakefpga

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"syscall"

	log "github.com/Sirupsen/logrus"
	"github.com/docker/docker/libaccelerator/driverapi"
	"github.com/docker/docker/libaccelerator/types"
)

type deviceInfo struct {
	configuredAccType string
	supportedAccType  []string
	bandwidth         int
	avail             bool
}

type driver struct {
	devices map[string]deviceInfo // map[pci] = deviceInfo
	slots   map[string]string     // map[sid] = runtime
}

// Init is the initializing function of fakefpga driver
func Init(dc driverapi.DriverCallback, config map[string]interface{}) (retErr error) {
	d := &driver{
		devices: make(map[string]deviceInfo),
		slots:   make(map[string]string),
	}

	// Prepare fake device and library
	devPath := "/dev/cloud_acc_vf"
	libPath := []string{
		"/var/lib/cloud_acc_vf_driver/bin",
		"/var/lib/cloud_acc_vf_driver/lib",
	}
	if err := syscall.Mknod(devPath,
		syscall.S_IREAD|syscall.S_IWRITE|syscall.S_IFCHR,
		99<<16|99); err != nil && err != syscall.EEXIST {
		return types.ForbiddenErrorf("can not create char dev %s", devPath)
	}
	defer func() {
		if retErr != nil {
			os.Remove(devPath)
		}
	}()
	for _, p := range libPath {
		if err := os.MkdirAll(p, 0755); err != nil && err != os.ErrExist {
			return types.ForbiddenErrorf("failed to create lib dir %s: %v", p, err)
		}
		defer func(path string) {
			if retErr != nil {
				os.RemoveAll(path)
			}
		}(p)
	}

	// Total 4* FakeFPGA devices
	device := deviceInfo{
		configuredAccType: "",
		supportedAccType:  d.Runtimes(),
		bandwidth:         500000,
		avail:             true,
	}
	d.devices["00ff:06:04.1"] = device
	d.devices["00ff:06:04.2"] = device
	d.devices["00ff:06:04.3"] = device
	d.devices["00ff:06:04.4"] = device

	// recovery Slots from controller
	slots, err := dc.QueryManagedSlots(d.Name())
	if err != nil {
		return err
	}
	for _, slot := range slots {
		dev := d.devices[slot.Device]
		dev.configuredAccType = slot.Runtime
		dev.avail = false
		d.devices[slot.Device] = dev
		d.slots[slot.Sid] = slot.Device
	}

	// register driver to controller
	c := driverapi.Capability{Runtimes: d.Runtimes()}
	if err := dc.RegisterDriver(d.Name(), d, c, []driverapi.SlotInfo{}); err != nil {
		log.Errorf("%s: error registering driver: %v", d.Name(), err)
		return err
	}
	return nil
}

// Name return driver name of fakefpga
func (d *driver) Name() string {
	return "fakefpga"
}

// Runtimes returns the runtime supported by fakefpga
func (d *driver) Runtimes() []string {
	return []string{"ipsec.dh", "ipsec.aes", "snow3g"}
}

// QueryRuntime checks whether specified runtime supported by fakefpga
func (d *driver) QueryRuntime(runtime string) error {
	for _, r := range d.Runtimes() {
		if r == runtime {
			return nil
		}
	}
	return types.NotImplementedErrorf("runtime \"%s\" not implement", runtime)
}

// ListDevice lists all the device fakefpga has
func (d *driver) ListDevice() ([]driverapi.DeviceInfo, error) {
	devices := make([]driverapi.DeviceInfo, 0)
	for pci, info := range d.devices {
		info := driverapi.DeviceInfo{
			SupportedRuntimes: info.supportedAccType,
			DeviceIdentify:    pci,
			Capacity: map[string]string{
				"bandwidth": fmt.Sprintf("%d", info.bandwidth),
			},
			Status: "available",
		}
		devices = append(devices, info)
	}
	return devices, nil
}

// AllocateSlot is used to allocate a new fakefpga slot with specifed sid, runtime and options
func (d *driver) AllocateSlot(sid, runtime string, options []string) error {
	if sid == "" {
		return types.BadRequestErrorf("slot id can't be empty")
	}

	slotOpts, err := d.parseFpgaSlotOptions(options)
	if err != nil {
		return types.BadRequestErrorf("bad options: %v", err)
	}

	// allocate slot
	pci := ""
	for p, dev := range d.devices {
		if slotOpts.Device != "" {
			if p != slotOpts.Device {
				continue
			} else if !dev.avail {
				return types.NoServiceErrorf("device \"%s\" busy", slotOpts.Device)
			}
		}

		if dev.avail {
			pci = p
			break
		}
	}

	if pci == "" {
		if slotOpts.Device != "" {
			return types.NoServiceErrorf("device \"%s\" not found", slotOpts.Device)
		} else {
			return types.NoServiceErrorf("no available device for %s", sid)
		}
	}

	dev := d.devices[pci]
	dev.configuredAccType = runtime
	dev.avail = false

	d.devices[pci] = dev
	d.slots[sid] = pci

	return nil
}

// ReleaseSlot is used to release slot specifed by sid
func (d *driver) ReleaseSlot(sid string) error {
	if _, ok := d.slots[sid]; !ok {
		return types.NotFoundErrorf("slot %s not found", sid)
	}
	pci := d.slots[sid]
	dev := d.devices[pci]

	// Release slot
	dev.configuredAccType = ""
	dev.avail = true
	d.devices[pci] = dev
	delete(d.slots, sid)

	log.Debugf("%s: release slot %s with ID: %s", d.Name(), pci, sid)

	return nil
}

// ListSlot lists all the slots fakefpga has
func (d *driver) ListSlot() ([]string, error) {
	ret := []string{}
	for sid, _ := range d.slots {
		ret = append(ret, sid)
	}
	return ret, nil
}

// Slot returns the slot information of specified sid
func (d *driver) Slot(sid string) (*driverapi.SlotInfo, error) {
	if pci, ok := d.slots[sid]; ok {
		return &driverapi.SlotInfo{
			Sid:     sid,
			Name:    "fake-fpga-dev",
			Device:  pci,
			Runtime: d.devices[pci].configuredAccType,
		}, nil
	} else {
		return nil, types.NotFoundErrorf("slot %s not found", sid)
	}
}

// PrepareSlot is used to provide runtime environment for container to use specified slot
func (d *driver) PrepareSlot(sid string) (*driverapi.SlotConfig, error) {
	sConfig := &driverapi.SlotConfig{
		Envs: make(map[string]string),
	}

	sConfig.Devices = []string{"/dev/cloud_acc_vf"}
	sConfig.Binds = append(sConfig.Binds, driverapi.Mount{
		Source:      "/var/lib/cloud_acc_vf_driver",
		Destination: "/usr/local/cloud_acc_vf_driver",
		Mode:        "ro",
	})
	sConfig.Envs["LD_LIBRARY_PATH"] = "/usr/local/cloud_acc_vf_driver/lib"
	sConfig.Envs["PATH"] = "/usr/local/cloud_acc_vf_driver/bin"

	return sConfig, nil
}

// FpgaSlotOption defines the options supported by fpga slot
type FpgaSlotOption struct {
	Device    string
	Bandwidth int
}

func (d *driver) parseFpgaSlotOptions(options []string) (*FpgaSlotOption, error) {
	slotOpts := &FpgaSlotOption{
		Device:    "",
		Bandwidth: 0,
	}

	for _, opt := range options {
		pair := strings.SplitN(opt, "=", 2)
		if len(pair) == 2 {
			if pair[0] == "device" {
				slotOpts.Device = pair[1]
			} else if pair[0] == "bandwidth" {
				if bw, err := strconv.Atoi(pair[1]); err == nil {
					slotOpts.Bandwidth = bw
				} else {
					return nil, fmt.Errorf("invalid bandwidth \"%s\"", pair[1])
				}
			} else {
				log.Infof("%s: ignore unknown slot option \"%s\"", d.Name(), opt)
			}
		} else {
			log.Infof("%s: ignore unknown slot option \"%s\"", d.Name(), opt)
		}
	}

	return slotOpts, nil
}

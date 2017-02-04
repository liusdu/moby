package driverapi

// AcceleratorPluginEndpointType represents the Endpoint Type used by Plugin system
const AcceleratorPluginEndpointType = "AcceleratorDriver"

// DeviceInfo indicates the attributes of Device
type DeviceInfo struct {
	SupportedRuntimes []string
	DeviceIdentify    string
	Capacity          map[string]string
	Status            string
}

// SlotInfo indicates the attributes of Slot
type SlotInfo struct {
	Name    string
	Device  string
	Runtime string
}

// Mount defines the mount info slot provided to container
type Mount struct {
	Source      string
	Destination string
	Mode        string
}

// SlotConfig defines the configuration container needed to use slot in it.
type SlotConfig struct {
	Binds   []Mount
	Envs    map[string]string
	Devices []string
}

// Driver is an interface that every plugin driver needs to implements.
//   sid - Accelerator Slot ID
type Driver interface {
	// Name returns the name of this driver
	Name() string

	// Runtimes returns the accelerator runtimes supported by this driver
	// e.g. ["cuda", "opencl"]
	Runtimes() []string

	// QueryRuntime check if a runtime is supported, e.g. "cuda:7.5"
	QueryRuntime(runtime string) error

	// ListDevice list all the devices managed by this driver
	ListDevice() ([]DeviceInfo, error)

	// AllocateSlot invokes the driver method to allocate an accelerator
	// resource slot with the requested slot id and runtime.
	AllocateSlot(sid, runtime string, options []string) error

	// Release accelerator resource slot
	ReleaseSlot(sid string) error

	// ListSlot list all the slots in this driver
	ListSlot() ([]string, error)

	// Slot returns the specific slot information
	Slot(sid string) (*SlotInfo, error)

	// Prepare
	PrepareSlot(sid string) (*SlotConfig, error)
}

// DriverCallback defines an interface to maintainer driver infomation
type DriverCallback interface {
	RegisterDriver(name string, driver Driver, capability Capability) error
}

// Capability defines the capability driver provided, here means runtime it support
type Capability struct {
	Runtimes []string
}

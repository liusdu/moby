package accelerator

import (
	// TODO return types need to be refactored into pkg
	"github.com/docker/engine-api/types"
)

// Backend is the methods that need to be implemented to provide
// accelerator specific functionality
type Backend interface {
	// Accels lists known accelerators
	Accels() ([]*types.Accel, []string, error)
	// AccelDrivers lists known accelerator drivers
	AccelDrivers() ([]*types.AccelDriver, []string, error)
	// AccelDevices lists all accelerators managed by selected driver
	AccelDevices(driver string) ([]types.AccelDevice, []string, error)
	// AccelInspect looks up an accelerator by name, error occurs when accelrator not found
	AccelInspect(name string) (*types.Accel, error)
	// AccelCreate create accelerator with specified requests
	AccelCreate(name, driverName string, runtime string, options []string) (*types.Accel, error)
	// AccelRm remove specified accelerators
	AccelRm(name string, force bool) error
}

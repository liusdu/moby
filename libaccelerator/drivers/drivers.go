package drivers

import (
	"github.com/docker/docker/libaccelerator/drivers/fakefpga"
	"github.com/docker/docker/libaccelerator/drivers/remote"
	"github.com/docker/docker/libaccelerator/drvregistry"
)

// DriverInitializer defines the struct of accelerator driver
type DriverInitializer struct {
	// InitFn is the function used to initialize driver
	InitFn drvregistry.InitFunc
	// DrvType specifies the type of current driver
	DrvType string
}

// GetInitializers returns the initializer of accelerator drivers
func GetInitializers() []DriverInitializer {
	in := []DriverInitializer{
		{fakefpga.Init, "fakefpga"},
		{remote.Init, "remote"},
	}
	return in
}

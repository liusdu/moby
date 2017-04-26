// +build accdrv

package drivers

import (
	"github.com/docker/docker/libaccelerator/drivers/fakefpga"
)

func additionalDrivers() []DriverInitializer {
	return []DriverInitializer{
		{fakefpga.Init, "fakefpga"},
	}
}

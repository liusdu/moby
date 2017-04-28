// +build accdrv

package fakefpga

import (
	log "github.com/Sirupsen/logrus"
	"github.com/docker/docker/libaccelerator/driverapi"
)

// Init is the initializing function of fakefpga driver
func Init(dc driverapi.DriverCallback, config map[string]interface{}) error {
	d, err := NewDriver()
	if err != nil {
		return err
	}

	// recovery Slots from controller
	slots, err := dc.QueryManagedSlots(d.Name())
	if err != nil {
		return err
	}
	invalidSlots, err := d.SyncState(slots)
	if err != nil {
		return err
	}

	// register driver to controller
	c := driverapi.Capability{Runtimes: d.Runtimes()}
	if err := dc.RegisterDriver(d.Name(), d, c, invalidSlots); err != nil {
		log.Errorf("%s: error registering driver: %v", d.Name(), err)
		return err
	}
	return nil
}

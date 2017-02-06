package daemon

import (
	"fmt"

	log "github.com/Sirupsen/logrus"
	"github.com/docker/docker/libaccelerator"
	"github.com/docker/docker/libaccelerator/driverapi"
	"github.com/docker/docker/pkg/plugins"
)

func (daemon *Daemon) initAccelController(config *Config) (libaccelerator.AcceleratorController, error) {
	log.Debugf("Initialize accelerator controller")

	// Create libaccelerator controller
	controller, err := libaccelerator.New(daemon.root)
	if err != nil {
		return nil, fmt.Errorf("error obtaining controller instance: %v", err)
	}

	// Preload requested driver
	for _, name := range config.AccelDriverPlugins {
		log.Debugf("  Loading accelerator plugin: %s", name)
		_, err := plugins.Get(name, driverapi.AcceleratorPluginEndpointType)
		if err != nil {
			return nil, fmt.Errorf("error loading accelerator driver plugin \"%s\": %v", name, err)
		}
	}

	// Cleanup invalid accelerators
	controller.CleanupSlots(nil)

	return controller, nil
}

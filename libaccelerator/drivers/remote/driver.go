package remote

import (
	"sync"

	log "github.com/Sirupsen/logrus"
	"github.com/docker/docker/libaccelerator/driverapi"
	"github.com/docker/docker/libaccelerator/drivers/remote/api"
	"github.com/docker/docker/pkg/plugins"
)

type PluginEndpoint interface {
	Call(serviceMethod string, args interface{}, ret interface{}) error
}

type driver struct {
	sync.Mutex
	endpoint   PluginEndpoint
	driverName string
	SeqNo      int
}

type maybeError interface {
	GetError() error
}

func newDriver(name string, endpoint PluginEndpoint) driverapi.Driver {
	return &driver{driverName: name, endpoint: endpoint, SeqNo: 0}
}

// Init is the initialzing function of remote driver, to get and register all the driver through docker plugin
func Init(dc driverapi.DriverCallback, config map[string]interface{}) error {
	plugins.Handle(driverapi.AcceleratorPluginEndpointType, func(name string, client *plugins.Client) {
		d := newDriver(name, client)
		c, err := d.(*driver).getCapabilities()
		if err != nil {
			log.Errorf("error getting capability for %s due to %v", name, err)
			return
		}
		if err = dc.RegisterDriver(name, d, *c); err != nil {
			log.Errorf("error registering driver for %s due to %v", name, err)
		}
	})
	return nil
}

func (d *driver) getCapabilities() (*driverapi.Capability, error) {
	var capResp api.GetCapabilityResponse
	if err := d.call("GetCapability", nil, &capResp); err != nil {
		return nil, err
	}

	c := &driverapi.Capability{Runtimes: capResp.Runtimes}

	return c, nil
}

func (d *driver) call(methodName string, arg interface{}, retVal maybeError) (retErr error) {
	method := driverapi.AcceleratorPluginEndpointType + "." + methodName

	d.Lock()
	defer func() {
		d.SeqNo++
		d.Unlock()
		// check if we need recovery from ERR_RESP_NOTSYNC
		if _, ok := retErr.(*driverapi.ErrNotSync); ok {
			retErr = d.recall(method, arg, retVal)
		}
	}()

	req := api.Request{SeqNo: d.SeqNo, Args: arg}
	if err := d.endpoint.Call(method, req, retVal); err != nil {
		return err
	}

	return retVal.GetError()
}

func (d *driver) recall(method string, arg interface{}, retVal maybeError) error {
	// Plugin & Daemon need state sync
	// do state sync if plugin return ErrNotSync
	if _, err := d.getCapabilities(); err != nil {
		return err
	}

	d.Lock()
	defer func() {
		d.SeqNo++
		d.Unlock()
	}()

	// restart call
	req := api.Request{SeqNo: d.SeqNo, Args: arg}
	if err := d.endpoint.Call(method, req, retVal); err != nil {
		return err
	}

	// if the restart call still failed, just return it.

	return retVal.GetError()
}

// Name returns the name of this driver
func (d *driver) Name() string {
	return d.driverName
}

// Runtimes returns the accelerator runtimes supported by this driver
// e.g. ["cuda", "opencl"]
func (d *driver) Runtimes() []string {
	var resp api.GetRuntimesResponse
	if err := d.call("GetRuntime", nil, &resp); err != nil {
		return []string{}
	}
	return resp.Runtimes
}

// QueryRuntime check if a runtime is supported, e.g. "cuda:7.5"
func (d *driver) QueryRuntime(runtime string) error {
	query := &api.QueryRuntimeRequest{Runtime: runtime}
	return d.call("QueryRuntime", query, &api.QueryRuntimeResponse{})
}

// ListDevice list all the devices managed by this driver
func (d *driver) ListDevice() ([]driverapi.DeviceInfo, error) {
	var resp api.ListDeviceResponse
	if err := d.call("ListDevice", nil, &resp); err != nil {
		return nil, err
	}
	return resp.Devices, nil
}

// AllocateSlot invokes the driver method to allocate an accelerator
// resource slot with the requested slot id and runtime.
func (d *driver) AllocateSlot(sid, runtime string, options []string) error {
	req := &api.AllocateSlotRequest{
		SlotID:  sid,
		Runtime: runtime,
		Options: options,
	}
	return d.call("AllocateSlot", req, &api.AllocateSlotResponse{})
}

// Release accelerator resource slot
func (d *driver) ReleaseSlot(sid string) error {
	req := &api.ReleaseSlotRequest{SlotID: sid}
	return d.call("ReleaseSlot", req, &api.ReleaseSlotResponse{})
}

// ListSlot list all the slots in this driver
func (d *driver) ListSlot() ([]string, error) {
	var resp api.ListSlotResponse
	if err := d.call("ListSlot", nil, &resp); err != nil {
		return nil, err
	}
	return resp.Slots, nil
}

// Slot returns the specific slot information
func (d *driver) Slot(sid string) (*driverapi.SlotInfo, error) {
	var resp api.SlotInfoResponse
	req := api.SlotInfoRequest{SlotID: sid}
	if err := d.call("SlotInfo", &req, &resp); err != nil {
		return nil, err
	}
	return &resp.SlotInfo, nil
}

// PrepareSlot does slot prepare to get slot runtime environment ready
func (d *driver) PrepareSlot(sid string) (*driverapi.SlotConfig, error) {
	var resp api.PrepareSlotResponse
	req := api.PrepareSlotRequest{SlotID: sid}
	if err := d.call("PrepareSlot", &req, &resp); err != nil {
		return nil, err
	}
	return &resp.SlotConfig, nil
}

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
	dc         driverapi.DriverCallback
	endpoint   PluginEndpoint
	driverName string
	SeqNo      int
}

type maybeError interface {
	GetError() error
}

func newDriver(name string, dc driverapi.DriverCallback, endpoint PluginEndpoint) driverapi.Driver {
	return &driver{
		driverName: name,
		dc:         dc,
		endpoint:   endpoint,
		SeqNo:      0,
	}
}

// Init is the initialzing function of remote driver, to get and register all the driver through docker plugin
func Init(dc driverapi.DriverCallback, config map[string]interface{}) error {
	plugins.Handle(driverapi.AcceleratorPluginEndpointType, func(name string, client *plugins.Client) {
		d := newDriver(name, dc, client)
		cap, slots, err := d.(*driver).getCapabilities()
		if err != nil {
			log.Errorf("error getting capability for %s due to %v", name, err)
			return
		}
		if err = dc.RegisterDriver(name, d, *cap, *slots); err != nil {
			log.Errorf("error registering driver for %s due to %v", name, err)
		}
	})
	return nil
}

func (d *driver) getCapabilities() (*driverapi.Capability, *[]driverapi.SlotInfo, error) {
	// walk slots, find all slots managed by this driver
	slots, err := d.dc.QueryManagedSlots(d.driverName)
	if err != nil {
		return nil, nil, err
	}

	// send list to plugin, force state sync
	capReq := api.GetCapabilityRequest{Slots: slots}
	capResp := api.GetCapabilityResponse{}
	if err := d.call("GetCapability", &capReq, &capResp); err != nil {
		return nil, nil, err
	}

	// update driver capability
	cap := driverapi.Capability{Runtimes: capResp.Runtimes}

	return &cap, &capResp.Slots, nil
}

func (d *driver) call(methodName string, arg interface{}, retVal maybeError) error {
	// build method URL
	methodCall := driverapi.AcceleratorPluginEndpointType + "." + methodName
	methodSync := driverapi.AcceleratorPluginEndpointType + "." + "GetCapability"

	// protect plugin call with lock
	d.Lock()
	defer func() {
		d.SeqNo++
		d.Unlock()
	}()

	// -- STEP #1: request plugin
	req := api.Request{SeqNo: d.SeqNo, Args: arg}
	if err := d.endpoint.Call(methodCall, req, retVal); err != nil {
		// endpoint error, just return
		return err
	}
	if retErr := retVal.GetError(); retErr == nil {
		// if everything is ok, return nil
		return nil
	} else if _, ok := retErr.(*driverapi.ErrNotSync); !ok {
		// return unrecoverable error
		return retVal.GetError()
	}
	// increase SeqNo for next plugin request
	d.SeqNo++

	// -- STEP #2: recovery from ERR_RESP_NOTSYNC
	err := func() error {
		// walk slots, find all slots managed by this driver
		slots, err := d.dc.QueryManagedSlots(d.driverName)
		if err != nil {
			return err
		}

		log.Warnf("d.dc.QueryManagedSlots(%s) return %v", d.driverName, slots)

		// send list to plugin, force state sync
		capReq := api.GetCapabilityRequest{Slots: slots}
		capResp := api.GetCapabilityResponse{}
		if err := d.endpoint.Call(methodSync,
			api.Request{SeqNo: d.SeqNo, Args: &capReq},
			&capResp); err != nil {
			return err
		}
		cap := driverapi.Capability{Runtimes: capResp.Runtimes}

		// notify libaccelerator controller to update slots info for driver
		return d.dc.UpdateDriver(d.driverName, cap, capResp.Slots)
	}()
	if err != nil {
		return err
	}
	// increase SeqNo for next plugin request
	d.SeqNo++

	// -- STEP #3: restart call
	// if the restart call still failed, just return it.
	req = api.Request{SeqNo: d.SeqNo, Args: arg}
	if err := d.endpoint.Call(methodCall, req, retVal); err != nil {
		return err
	}
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

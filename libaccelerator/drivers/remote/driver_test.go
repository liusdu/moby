package remote

import (
	"fmt"
	"testing"

	"github.com/docker/docker/libaccelerator/driverapi"
	"github.com/docker/docker/libaccelerator/drivers/remote/api"
)

type TestPlugin struct {
	SeqNo int
}

func (plugin *TestPlugin) Call(serviceMethod string, args interface{}, ret interface{}) error {
	defer func() { plugin.SeqNo++ }()

	req, ok := args.(api.Request)
	if !ok {
		return fmt.Errorf("bad request")
	}

	if serviceMethod == "AcceleratorDriver.GetCapability" {
		resp, ok := ret.(*api.GetCapabilityResponse)
		if !ok {
			return fmt.Errorf("bad request")
		}
		capReq, ok := req.Args.(*api.GetCapabilityRequest)
		if !ok {
			return fmt.Errorf("internal error: covert GetCapabilityRequest failed")
		}

		// reset plugin SeqNo
		plugin.SeqNo = req.SeqNo

		// check Slot list
		if len(capReq.Slots) != 3 {
			return fmt.Errorf("invalid request args")
		}

		// feed resp Slot list
		resp.Slots = []driverapi.SlotInfo{
			{Name: "slot0", Device: "dev0", Runtime: "rt0"},
		}

		resp.ErrType = api.RESP_ERR_NOERROR
		resp.ErrMsg = ""

	} else if serviceMethod == "AcceleratorDriver.SlotInfo" {
		resp, ok := ret.(*api.SlotInfoResponse)
		if !ok {
			return fmt.Errorf("bad request")
		}
		resp.ErrType = api.RESP_ERR_NOERROR
		resp.ErrMsg = ""

		// check SeqNo
		if plugin.SeqNo != req.SeqNo {
			resp.ErrType = api.RESP_ERR_NOTSYNC
			resp.ErrMsg = fmt.Sprintf("%d", plugin.SeqNo)
			return nil
		}

		// get SlotInfo request args
		siReq, ok := req.Args.(*api.SlotInfoRequest)
		if !ok {
			// this is interanl error, so return it by retval
			return fmt.Errorf("interal error: covert SlotInfoRequest failed")
		}

		// bad SlotID
		if siReq.SlotID != "sid" {
			resp.ErrType = api.RESP_ERR_NOTFOUND
			resp.ErrMsg = siReq.SlotID
		}
	} else {
		return driverapi.ErrNotImplemented(serviceMethod)
	}

	return nil
}

type TestController struct{}

func (c *TestController) RegisterDriver(driverName string, driver driverapi.Driver, cap driverapi.Capability, slots []driverapi.SlotInfo) error {
	return nil
}
func (c *TestController) UpdateDriver(driverName string, cap driverapi.Capability, slots []driverapi.SlotInfo) error {
	if len(slots) != 1 {
		return fmt.Errorf("UpdateDriver need receive only 1-slot")
	}
	return nil
}
func (c *TestController) QueryManagedSlots(driverName string) ([]driverapi.SlotInfo, error) {
	si := []driverapi.SlotInfo{
		{Name: "slot0", Device: "dev0", Runtime: "rt0"},
		{Name: "slot1", Device: "dev1", Runtime: "rt1"},
		{Name: "slot2", Device: "dev2", Runtime: "rt2"},
	}
	return si, nil
}

func TestAccelRemoteDriverSeqNo(t *testing.T) {
	plugin := &TestPlugin{SeqNo: 0}
	dc := &TestController{}
	d := newDriver("test-remote", dc, plugin)
	for i := 0; i < 10; i = i + 1 {
		if _, err := d.Slot("sid"); err != nil {
			t.Errorf("%v", err)
		}
	}

	// reset driver.SeqNo, expect error
	d.(*driver).SeqNo = 0
	for i := 0; i < 10; i = i + 1 {
		if _, err := d.Slot("sid"); err != nil {
			t.Errorf("%v", err)
		}
	}

	// reset plugin.SeqNo, expect error
	plugin.SeqNo = 0
	for i := 0; i < 10; i = i + 1 {
		if _, err := d.Slot("sid"); err != nil {
			t.Errorf("%v", err)
		}
	}
}

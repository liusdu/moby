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
		plugin.SeqNo = req.SeqNo
	} else if serviceMethod == "AcceleratorDriver.SlotInfo" {
		resp, ok := ret.(*api.SlotInfoResponse)
		if !ok {
			return fmt.Errorf("bad request")
		}
		resp.ErrType = 0
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
		return &driverapi.ErrNotImplemented{}
	}

	return nil
}

func TestAccelRemoteDriverSeqNo(t *testing.T) {
	plugin := &TestPlugin{SeqNo: 0}
	d := newDriver("test-remote", plugin)
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

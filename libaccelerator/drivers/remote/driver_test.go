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

	if serviceMethod == "AcceleratorDriver.SlotInfo" {
		req, ok := args.(api.Request)
		if !ok {
			return fmt.Errorf("bad request")
		}
		if plugin.SeqNo != req.SeqNo {
			return fmt.Errorf("mismatch SeqNo: Request.SeqNo=%d, Plugin.SeqNo=%d", req.SeqNo, plugin.SeqNo)
		}

		siReq, ok := req.Args.(*api.SlotInfoRequest)
		if !ok {
			return fmt.Errorf("covert SlotInfoRequest failed")
		}
		if siReq.SlotID != "sid" {
			return fmt.Errorf("bad SlotInfoRequest.SlotID: %s", siReq.SlotID)
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
	if _, err := d.Slot("sid"); err == nil {
		t.Errorf("reset driver.SeqNo check failed")
	}

	// reset plugin.SeqNo, expect error
	plugin.SeqNo = 0
	if _, err := d.Slot("sid"); err == nil {
		t.Errorf("reset plugin.SeqNo check failed")
	}
}

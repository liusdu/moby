package api

import (
	"fmt"

	"github.com/docker/docker/libaccelerator/driverapi"
)

// Request is the request head for plugin call
type Request struct {
	SeqNo int         // current sequence number when send the request
	Args  interface{} // args
}

// Response is the basic response structure used in all responses.
type Response struct {
	ErrType int
	ErrMsg  string
}

const (
	// code 0x0[0-9]: for plugin internal error
	RESP_ERR_NOERROR = 0x0
	RESP_ERR_NOTIMPL = 0x1
	RESP_ERR_NOTSYNC = 0x2
	// code 0x1[0-9]: for accelerator operation error
	RESP_ERR_NOTFOUND = 0x10
	RESP_ERR_NODEV    = 0x11
)

// GetError returns the error from the response, if any.
func (r *Response) GetError() error {
	if r.ErrType == RESP_ERR_NOERROR {
		return nil
	} else if r.ErrType == RESP_ERR_NOTIMPL {
		return &driverapi.ErrNotImplemented{}
	} else if r.ErrType == RESP_ERR_NOTSYNC {
		return &driverapi.ErrNotSync{}
	} else if r.ErrType == RESP_ERR_NOTFOUND {
		return driverapi.ErrNoSlot(r.ErrMsg)
	} else if r.ErrType == RESP_ERR_NODEV {
		return driverapi.ErrNoDev(r.ErrMsg)
	} else {
		return fmt.Errorf("remote: %s", r.ErrMsg)
	}
}

// GetCapabilityRequest defines the request content GetCapability needed
type GetCapabilityRequest struct {
	Slots []driverapi.SlotInfo
}

// GetCapabilityResponse is the response of GetCapability request.
type GetCapabilityResponse struct {
	Response
	Runtimes []string
	Slots    []driverapi.SlotInfo
}

// GetRuntimesResponse is the response of GetRuntimes request
type GetRuntimesResponse struct {
	Response
	Runtimes []string
}

// QueryRuntimeRequest defines the request content runtime query needed
type QueryRuntimeRequest struct {
	Runtime string
}

// QueryRuntimeResponse defines the response content of runtime query
type QueryRuntimeResponse struct{ Response }

// ListDeviceResponse defines the response content of device list, the device info of specified driver
type ListDeviceResponse struct {
	Response
	Devices []driverapi.DeviceInfo
}

// AllocateSlotRequest requests a new accelerator slot.
type AllocateSlotRequest struct {
	// A accelerator slot ID that remote plugins are expected to store for future
	// reference.
	SlotID string

	// The request accelerator runtime for allocated slot.
	Runtime string

	// Extra options for accelerator plugin.
	Options []string
}

// AllocateSlotResponse is the response to the AllocateSlotRequest.
type AllocateSlotResponse struct {
	Response
}

// ReleaseSlotRequest is the request to release an accelerator slot.
type ReleaseSlotRequest struct {
	// The ID of the accelerator slot to release
	SlotID string
}

// ReleaseSlotResponse is the response to the ReleaseSlotRequest.
type ReleaseSlotResponse struct {
	Response
}

// ListSLotResponse is the response of slot list, returns the slot driver contains
type ListSlotResponse struct {
	Response
	Slots []string
}

// SlotInfoRequest defines the request to get slot info
type SlotInfoRequest struct {
	SlotID string
}

// SlotInfoResonse is the response of slot info
type SlotInfoResponse struct {
	Response
	SlotInfo driverapi.SlotInfo
}

// PrepareSlotRequest is the request info to implement prepare slot
type PrepareSlotRequest struct {
	SlotID string
}

// PrepareSlotResonse is the reponse of prepare slot.
type PrepareSlotResponse struct {
	Response
	SlotConfig driverapi.SlotConfig
}

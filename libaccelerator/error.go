package libaccelerator

import (
	"fmt"
)

// ErrNoSuchSlot is returned when a slot query finds no result
type ErrNoSuchSlot string

func (nss ErrNoSuchSlot) Error() string {
	return fmt.Sprintf("No such slot: %s", string(nss))
}

// NotFound denotes the type of this error
func (nss ErrNoSuchSlot) NotFound() {}

// ErrInvalidAccelDriver is returned if an invalid driver
// name is passed.
type ErrInvalidAccelDriver string

func (ind ErrInvalidAccelDriver) Error() string {
	return fmt.Sprintf("Invalid driver bound to accelerator: %s", string(ind))
}

// BadRequest denotes the type of this error
func (ind ErrInvalidAccelDriver) BadRequest() {}

// ErrInvalidID is returned when a query-by-id method is being invoked
// with an empty id parameter
type ErrInvalidID string

func (ii ErrInvalidID) Error() string {
	return fmt.Sprintf("Invalid accelerator slot ID: \"%s\"", string(ii))
}

// BadRequest denotes the type of this error
func (ii ErrInvalidID) BadRequest() {}

// ErrInvalidName is returned when a query-by-name or resource create method is
// invoked with an empty name parameter
type ErrInvalidName string

func (in ErrInvalidName) Error() string {
	return fmt.Sprintf("Invalid name: %s", string(in))
}

// BadRequest denotes the type of this error
func (in ErrInvalidName) BadRequest() {}

// RuntimeTypeError type is returned when the accelerator runtime string is not
// known to accelerator.
type RuntimeTypeError string

func (nt RuntimeTypeError) Error() string {
	return fmt.Sprintf("Invalid runtime %q", string(nt))
}

// NotFound denotes the type of this error
func (nt RuntimeTypeError) NotFound() {}

// SlotNameError is returned when an accelerator with the same name already exists.
type SlotNameError string

func (nnr SlotNameError) Error() string {
	return fmt.Sprintf("Slot with name %s already exists", string(nnr))
}

// Forbidden denotes the type of this error
func (nnr SlotNameError) Forbidden() {}

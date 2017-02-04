package driverapi

import (
	"fmt"
)

// ErrNoSlot is returned if no accelerator slot with the specified id exists
type ErrNoSlot string

func (enn ErrNoSlot) Error() string {
	return fmt.Sprintf("No slot (%s) exists", string(enn))
}

// NotFound denotes the type of this error
func (enn ErrNoSlot) NotFound() {}

// ErrNotImplemented is returned when a Driver has not implemented an API yet
type ErrNotImplemented struct{}

func (eni *ErrNotImplemented) Error() string {
	return "The API is not implemented yet"
}

// NotImplemented denotes the type of this error
func (eni *ErrNotImplemented) NotImplemented() {}

// ErrActiveRegistration represents an error when a driver is registered to a networkType that is previously registered
type ErrActiveRegistration string

// Error interface for ErrActiveRegistration
func (ar ErrActiveRegistration) Error() string {
	return fmt.Sprintf("Driver already registered for type %q", string(ar))
}

// Forbidden denotes the type of this error
func (ar ErrActiveRegistration) Forbidden() {}

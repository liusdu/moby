package driverapi

import (
	"fmt"
)

/*
 * ErrNoSlot -> remote.api.RESP_ERR_NOUFOUND
 */

// ErrNoSlot is returned if no accelerator slot with the specified id exists
type ErrNoSlot string

func (ens ErrNoSlot) Error() string {
	return fmt.Sprintf("No slot (%s) exists", string(ens))
}

// NotFound denotes the type of this error
func (ens ErrNoSlot) NotFound() {}

/*
 * ErrNoDev -> remote.api.RESP_ERR_NODEV
 */

// ErrNoDev is returned if no accelerator device available
type ErrNoDev string

func (end ErrNoDev) Error() string {
	return fmt.Sprintf("No available device for %s", string(end))
}

// NoService denotes the type of this error
func (end ErrNoDev) NoService() {}

/*
 * ErrNotImplemented -> remote.api.RESP_ERR_NOTIMPL
 */

// ErrNotImplemented is returned when a Driver has not implemented an API yet
type ErrNotImplemented string

func (eni ErrNotImplemented) Error() string {
	return "The API is not implemented yet"
}

// NotImplemented denotes the type of this error
func (eni ErrNotImplemented) NotImplemented() {}

/*
 * ErrNotSync -> remote.api.RESP_ERR_NOSYNC
 */

// ErrNotSync s returned if driver is not sync with daemon (e.g. crash recovery)
type ErrNotSync string

// Error interface for ErrNotSync
func (ens ErrNotSync) Error() string {
	return "plugin not sync with daemon"
}

// Forbidden denotes the type of this error
func (ens ErrNotSync) Forbidden() {}

/*
 * ErrActiveRegistration represents an error when a driver is registered
 * to a accelerator type that is previously registered
 */
type ErrActiveRegistration string

// Error interface for ErrActiveRegistration
func (ar ErrActiveRegistration) Error() string {
	return fmt.Sprintf("Driver already registered for type %q", string(ar))
}

// Forbidden denotes the type of this error
func (ar ErrActiveRegistration) Forbidden() {}

package types

import (
	"fmt"
)

/******************************
 * Well-known Error Interfaces
 ******************************/

// MaskableError is an interface for errors which can be ignored by caller
type MaskableError interface {
	// Maskable makes implementer into MaskableError type
	Maskable()
}

// RetryError is an interface for errors which might get resolved through retry
type RetryError interface {
	// Retry makes implementer into RetryError type
	Retry()
}

// BadRequestError is an interface for errors originated by a bad request
type BadRequestError interface {
	// BadRequest makes implementer into BadRequestError type
	BadRequest()
}

// NotFoundError is an interface for errors raised because a needed resource is not available
type NotFoundError interface {
	// NotFound makes implementer into NotFoundError type
	NotFound()
}

// ForbiddenError is an interface for errors which denote a valid request that cannot be honored
type ForbiddenError interface {
	// Forbidden makes implementer into ForbiddenError type
	Forbidden()
}

// NoServiceError is an interface for errors returned when the required service is not available
type NoServiceError interface {
	// NoService makes implementer into NoServiceError type
	NoService()
}

// TimeoutError is an interface for errors raised because of timeout
type TimeoutError interface {
	// Timeout makes implementer into TimeoutError type
	Timeout()
}

// NotImplementedError is an interface for errors raised because of requested functionality is not yet implemented
type NotImplementedError interface {
	// NotImplemented makes implementer into NotImplementedError type
	NotImplemented()
}

// InternalError is an interface for errors raised because of an internal error
type InternalError interface {
	// Internal makes implementer into InternalError type
	Internal()
}

/******************************
 * Well-known Error Formatters
 ******************************/

// BadRequestErrorf creates an instance of BadRequestError
func BadRequestErrorf(format string, params ...interface{}) error {
	return badRequest(fmt.Sprintf(format, params...))
}

// NotFoundErrorf creates an instance of NotFoundError
func NotFoundErrorf(format string, params ...interface{}) error {
	return notFound(fmt.Sprintf(format, params...))
}

// ForbiddenErrorf creates an instance of ForbiddenError
func ForbiddenErrorf(format string, params ...interface{}) error {
	return forbidden(fmt.Sprintf(format, params...))
}

// NoServiceErrorf creates an instance of NoServiceError
func NoServiceErrorf(format string, params ...interface{}) error {
	return noService(fmt.Sprintf(format, params...))
}

// NotImplementedErrorf creates an instance of NotImplementedError
func NotImplementedErrorf(format string, params ...interface{}) error {
	return notImpl(fmt.Sprintf(format, params...))
}

// TimeoutErrorf creates an instance of TimeoutError
func TimeoutErrorf(format string, params ...interface{}) error {
	return timeout(fmt.Sprintf(format, params...))
}

// InternalErrorf creates an instance of InternalError
func InternalErrorf(format string, params ...interface{}) error {
	return internal(fmt.Sprintf(format, params...))
}

// InternalMaskableErrorf creates an instance of InternalError and MaskableError
func InternalMaskableErrorf(format string, params ...interface{}) error {
	return maskInternal(fmt.Sprintf(format, params...))
}

// RetryErrorf creates an instance of RetryError
func RetryErrorf(format string, params ...interface{}) error {
	return retry(fmt.Sprintf(format, params...))
}

/***********************
 * Internal Error Types
 ***********************/
type badRequest string

// Error returns the string content of badRequest
func (br badRequest) Error() string {
	return string(br)
}

// BadRequest is null, used to define a unique badRequest error
func (br badRequest) BadRequest() {}

type maskBadRequest string

type notFound string

// Error returns the string content of notFound
func (nf notFound) Error() string {
	return string(nf)
}

// NotFound is null, used to define a unique notFound error
func (nf notFound) NotFound() {}

type forbidden string

// Error returns the string content of forbidden
func (frb forbidden) Error() string {
	return string(frb)
}

// Forbidden is null, used to define a unique forbidden error
func (frb forbidden) Forbidden() {}

type noService string

// Error returns the string content of noService
func (ns noService) Error() string {
	return string(ns)
}

// NoService is null, used to define a unique noService error
func (ns noService) NoService() {}

type maskNoService string

type timeout string

// Error returns the string content of timeout
func (to timeout) Error() string {
	return string(to)
}

// Timeout is null, used to define a unique timeout error
func (to timeout) Timeout() {}

type notImpl string

// Error returns the string content of notImpl
func (ni notImpl) Error() string {
	return string(ni)
}

// NotImplemented is null, used to define a unique notImplemented error
func (ni notImpl) NotImplemented() {}

type internal string

// Error returns the string content of internal
func (nt internal) Error() string {
	return string(nt)
}

// Internel is null, used to define a unique internal error
func (nt internal) Internal() {}

type maskInternal string

// Error returns the string content of maskInternal
func (mnt maskInternal) Error() string {
	return string(mnt)
}

// Internel is null, used to implement a maskInternal interface
func (mnt maskInternal) Internal() {}

// Maskable is null, used to define a unique maskInternal error
func (mnt maskInternal) Maskable() {}

type retry string

// Error returns the string content of retry
func (r retry) Error() string {
	return string(r)
}

// Retry is null, used to define a unique retry error
func (r retry) Retry() {}

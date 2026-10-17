package domain

import "errors"

var (
	ErrNotFound               = errors.New("not found")
	ErrShipmentNotFound       = errors.New("shipment not found")
	ErrPaymentNotFound        = errors.New("payment not found")
	ErrUnauthorized           = errors.New("unauthorized")
	ErrForbidden              = errors.New("forbidden")
	ErrInvalidStateTransition = errors.New("invalid state transition")
	ErrIdempotencyConflict    = errors.New("idempotency conflict")
	ErrInvalidInput           = errors.New("invalid input")
	ErrAlreadyProcessed       = errors.New("already processed")
	ErrPaymentFailed          = errors.New("payment failed")
	ErrCannotCancel           = errors.New("shipment cannot be cancelled in current status")
)

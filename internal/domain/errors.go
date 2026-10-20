package domain

import "errors"

var (
	ErrBranchNotFound                = errors.New("branch not found")
	ErrServiceNotFound               = errors.New("service not found")
	ErrBranchDeleteBlockedByBookings = errors.New("branch delete blocked because it has active bookings")
	ErrServiceNotAvailableAtBranch   = errors.New("service is not available at this branch")
	ErrBookingNotFound               = errors.New("booking not found")
	ErrBookingSlotInPast             = errors.New("time slot is in the past")
	ErrAdminNotFound                 = errors.New("admin not found")
	ErrCapacityExceeded              = errors.New("branch capacity exceeded for this time slot")
	ErrIdempotencyKeyReused          = errors.New("idempotency key was used for a different request")
	ErrIdempotentBookingCancelled    = errors.New("the booking created with this idempotency key has been cancelled")
)

package domain

import "errors"

var (
	ErrBranchNotFound   = errors.New("branch not found")
	ErrServiceNotFound  = errors.New("service not found")
	ErrBookingNotFound  = errors.New("booking not found")
	ErrCapacityExceeded = errors.New("branch capacity exceeded for this time slot")
)

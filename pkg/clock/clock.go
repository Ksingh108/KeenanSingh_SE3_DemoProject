// Package clock abstracts the current time so that time-dependent rules, such
// as rejecting bookings in the past, can be tested deterministically.
package clock

import (
	"time"
	// Embed the IANA time zone database so the named location resolves on any
	// host, including minimal container images and Windows development machines.
	_ "time/tzdata"
)

// BusinessLocation is the time zone in which branches operate. "Today" and
// "the current time" for booking rules are evaluated here, not in UTC.
const BusinessLocation = "Africa/Johannesburg"

// Clock reports the current time.
type Clock interface {
	Now() time.Time
}

type businessClock struct {
	location *time.Location
}

// Now returns the current time in the business time zone.
func (c businessClock) Now() time.Time {
	return time.Now().In(c.location)
}

// NewBusinessClock returns a Clock that reports time in BusinessLocation.
// If the zone data cannot be loaded it falls back to a fixed UTC+2 offset,
// which is equivalent because South Africa does not observe daylight saving.
func NewBusinessClock() Clock {
	location, err := time.LoadLocation(BusinessLocation)
	if err != nil {
		location = time.FixedZone("SAST", 2*60*60)
	}
	return businessClock{location: location}
}

// Fixed is a Clock that always reports the same instant. It is intended for
// tests.
type Fixed time.Time

// Now returns the fixed instant.
func (f Fixed) Now() time.Time {
	return time.Time(f)
}

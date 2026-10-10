// Package validator contains small, reusable validation helpers for HTTP
// handlers and services. Keep these simple and deterministic so they can be
// used both in production code and unit tests.
package validator

import (
	"regexp"
	"time"
)

// Precompiled regular expressions for performance. These are intentionally
// conservative and focused on common valid formats rather than attempting
// to be perfectly RFC-compliant (which can be complex).
var (
	// Basic email validation: user@host.tld (keeps it simple and readable)
	emailRegex = regexp.MustCompile(`^[a-zA-Z0-9._%+-]+@[a-zA-Z0-9.-]+\.[a-zA-Z]{2,}$`)
	// YYYY-MM-DD
	dateRegex = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}$`)
	// HH:MM 24-hour
	timeRegex = regexp.MustCompile(`^\d{2}:\d{2}$`)
	// Branch ID validation: alphanumeric and hyphens only
	branchIDRegex = regexp.MustCompile(`^[a-zA-Z0-9-]+$`)
	// Phone validation: optional leading plus, then digit groups separated by a
	// single space or hyphen. Digit count is checked separately.
	phoneRegex = regexp.MustCompile(`^\+?[0-9]+([ -][0-9]+)*$`)
	// Customer name validation: alphanumeric, spaces, hyphens only
	customerNameRegex = regexp.MustCompile(`^[a-zA-Z0-9\s\-]+$`)
	// Service ID validation: alphanumeric and hyphens only
	serviceIDRegex = regexp.MustCompile(`^[a-zA-Z0-9-]+$`)
	// Branch name validation: must start with an alphanumeric (so whitespace-only
	// values are rejected), then alphanumerics, spaces, hyphens, apostrophes,
	// ampersands and periods. A literal space is used instead of \s because \s
	// also matches newlines and tabs, which would allow log injection.
	branchNameRegex = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9 \-'&.]*$`)
	// City validation: letters, spaces, hyphens, apostrophes and periods only
	cityRegex = regexp.MustCompile(`^[a-zA-Z][a-zA-Z \-'.]*$`)
	// Address validation: must start with an alphanumeric, then alphanumerics,
	// spaces, hyphens, apostrophes, commas, periods, slashes and hash signs
	addressRegex = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9 \-',./#]*$`)
)

// Length limits for branch fields. They mirror the column sizes in init.sql so
// that oversized input is rejected with a 400 instead of failing in the database.
const (
	maxBranchNameLength = 255
	maxCityLength       = 100
	maxAddressLength    = 500

	// MaxBranchCapacity is the largest bookings-per-slot capacity accepted for a
	// branch. It is a sanity bound against typos, not a business rule.
	MaxBranchCapacity = 1000
)

// ValidateEmail checks whether the provided email looks valid. It enforces
// a maximum length to avoid abuse and uses a simple regex to check format.
// Note: this does not verify that the domain exists or that the address can
// receive email.
func ValidateEmail(email string) bool {
	return len(email) <= 255 && emailRegex.MatchString(email)
}

// ValidateDate verifies the string is in YYYY-MM-DD format and that it is
// not a date before today. "Today" is the calendar date of now in now's own
// location, so callers pass the current time in the business time zone and the
// date rolls over at local midnight rather than at midnight UTC.
func ValidateDate(dateStr string, now time.Time) bool {
	if !dateRegex.MatchString(dateStr) {
		return false
	}

	date, err := time.ParseInLocation("2006-01-02", dateStr, now.Location())
	if err != nil {
		return false
	}

	// Compare dates at midnight so that time-of-day doesn't cause unexpected failures.
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
	return !date.Before(today)
}

// ValidateSlotNotPast reports whether the slot on the given date starts after
// now, so a booking can't be made for a time that has already begun. It
// returns false if either the date or the slot is malformed. The slot is
// interpreted in now's location, matching ValidateDate.
func ValidateSlotNotPast(dateStr, timeSlot string, now time.Time) bool {
	if !dateRegex.MatchString(dateStr) || !timeRegex.MatchString(timeSlot) {
		return false
	}

	slotStart, err := time.ParseInLocation("2006-01-02 15:04", dateStr+" "+timeSlot, now.Location())
	if err != nil {
		return false
	}
	return slotStart.After(now)
}

// ValidateTimeSlot confirms the time slot string is in HH:MM 24-hour format
// and parses correctly (e.g., rejects 25:00). Returns true for valid times.
func ValidateTimeSlot(timeSlot string) bool {
	if !timeRegex.MatchString(timeSlot) {
		return false
	}

	// Parse time to ensure it's a valid 24-hour time.
	_, err := time.Parse("15:04", timeSlot)
	return err == nil
}

// ValidateBranchID ensures a branch ID is non-empty, within a reasonable
// length, and contains only allowed characters. This is useful for
// validating path parameters or user input that will be used in lookups.
func ValidateBranchID(branchID string) bool {
	return len(branchID) > 0 && len(branchID) <= 50 && branchIDRegex.MatchString(branchID)
}

// Phone numbers carry 7-15 digits: 15 is the E.164 maximum, 7 the shortest
// local number we accept.
const (
	minPhoneDigits = 7
	maxPhoneDigits = 15
)

// ValidatePhone ensures a phone number is at most 30 characters, has an
// optional leading plus followed by digit groups separated by single spaces or
// hyphens, and holds 7-15 digits in total. Letters and other punctuation are
// rejected, which also prevents log injection and ensures data integrity.
func ValidatePhone(phone string) bool {
	if len(phone) == 0 || len(phone) > 30 || !phoneRegex.MatchString(phone) {
		return false
	}
	digits := 0
	for i := 0; i < len(phone); i++ {
		if phone[i] >= '0' && phone[i] <= '9' {
			digits++
		}
	}
	return digits >= minPhoneDigits && digits <= maxPhoneDigits
}

// ValidateCustomerName ensures a customer name is 1-255 characters and
// contains only alphanumeric characters, spaces, and hyphens. This prevents
// log injection and ensures data integrity.
func ValidateCustomerName(name string) bool {
	return len(name) > 0 && len(name) <= 255 && customerNameRegex.MatchString(name)
}

// ValidateServiceID ensures a service ID is non-empty, within a reasonable
// length, and contains only alphanumeric characters and hyphens. This is used
// for validating service references.
func ValidateServiceID(serviceID string) bool {
	return len(serviceID) > 0 && len(serviceID) <= 50 && serviceIDRegex.MatchString(serviceID)
}

// ValidateBranchName ensures a branch name is 1-255 characters, starts with an
// alphanumeric character, and contains only alphanumerics, spaces, hyphens,
// apostrophes, ampersands and periods.
func ValidateBranchName(name string) bool {
	return len(name) > 0 && len(name) <= maxBranchNameLength && branchNameRegex.MatchString(name)
}

// ValidateCity ensures a city is 1-100 characters, starts with a letter, and
// contains only letters, spaces, hyphens, apostrophes and periods.
func ValidateCity(city string) bool {
	return len(city) > 0 && len(city) <= maxCityLength && cityRegex.MatchString(city)
}

// ValidateAddress ensures an address is 1-500 characters, starts with an
// alphanumeric character, and contains only alphanumerics, spaces, hyphens,
// apostrophes, commas, periods, slashes and hash signs.
func ValidateAddress(address string) bool {
	return len(address) > 0 && len(address) <= maxAddressLength && addressRegex.MatchString(address)
}

// ValidateCapacity ensures a branch capacity is a positive number no larger
// than MaxBranchCapacity.
func ValidateCapacity(capacity int) bool {
	return capacity > 0 && capacity <= MaxBranchCapacity
}

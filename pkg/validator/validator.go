// Package validator contains small, reusable validation helpers for HTTP
// handlers and services. Keep these simple and deterministic so they can be
// used both in production code and unit tests.
package validator

import (
	"fmt"
	"regexp"
	"strings"
	"time"
	"unicode"
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
	// Resource ID validation: canonical 8-4-4-4-12 hex UUID, matching the
	// `format: uuid` declared for IDs in api/openapi.yaml
	identifierRegex = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)
	// Phone validation: optional leading plus, then digit groups separated by a
	// single space or hyphen. Digit count is checked separately.
	phoneRegex = regexp.MustCompile(`^\+?[0-9]+([ -][0-9]+)*$`)
	// Customer name validation: alphanumeric, literal spaces, hyphens only
	customerNameRegex = regexp.MustCompile(`^[a-zA-Z0-9 \-]+$`)
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

// Length limits for branch fields. They mirror the database column sizes so
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

const (
	firstBookableSlotMinute = 9 * 60
	lastBookableSlotMinute  = 17 * 60
	bookableSlotInterval    = 30
)

// BookableTimeSlots returns the fixed start-time grid shared by availability and booking creation.
func BookableTimeSlots() []string {
	slots := make([]string, 0, (lastBookableSlotMinute-firstBookableSlotMinute)/bookableSlotInterval+1)
	for minute := firstBookableSlotMinute; minute <= lastBookableSlotMinute; minute += bookableSlotInterval {
		slots = append(slots, fmt.Sprintf("%02d:%02d", minute/60, minute%60))
	}
	return slots
}

// IsBookableTimeSlot reports whether a valid HH:MM time belongs to the advertised availability grid.
func IsBookableTimeSlot(timeSlot string) bool {
	if !ValidateTimeSlot(timeSlot) {
		return false
	}
	parsed, _ := time.Parse("15:04", timeSlot)
	minute := parsed.Hour()*60 + parsed.Minute()
	return minute >= firstBookableSlotMinute && minute <= lastBookableSlotMinute &&
		(minute-firstBookableSlotMinute)%bookableSlotInterval == 0
}

// ValidateIdentifier ensures an ID is a canonical UUID (8-4-4-4-12 hex digits).
func ValidateIdentifier(id string) bool {
	return identifierRegex.MatchString(id)
}

// ValidateBranchID ensures a branch ID is a canonical UUID.
func ValidateBranchID(branchID string) bool {
	return ValidateIdentifier(branchID)
}

// NormalizePhone converts a South African phone number into a single canonical
// format: +27 followed by the local number without the leading 0.
// Examples accepted: +27 82 123 4567, 082 123 4567, 021 555 0100.
func NormalizePhone(phone string) string {
	if phone == "" || phone != strings.TrimSpace(phone) {
		return ""
	}
	if !phoneRegex.MatchString(phone) {
		return ""
	}

	var digits strings.Builder
	digits.Grow(len(phone))
	for _, ch := range phone {
		if unicode.IsDigit(ch) {
			digits.WriteRune(ch)
		}
	}
	value := digits.String()
	if value == "" || len(value) < 9 || len(value) > 11 {
		return ""
	}

	switch {
	case strings.HasPrefix(value, "27"):
		return "+" + value
	case strings.HasPrefix(value, "0"):
		return "+27" + value[1:]
	default:
		return "+27" + value
	}
}

// ValidatePhone ensures the supplied value is a common South African phone
// format and returns true only if it can be normalized to a canonical +27 form.
// This keeps the validator strict enough to reject malformed input while still
// accepting user-friendly values like 082 123 4567 and +27 82 123 4567.
func ValidatePhone(phone string) bool {
	return NormalizePhone(phone) != ""
}

// ValidateCustomerName ensures a customer name is 1-255 characters and
// contains only alphanumeric characters, spaces, and hyphens. This prevents
// log injection and ensures data integrity.
func ValidateCustomerName(name string) bool {
	return len(name) > 0 && len(name) <= 255 && customerNameRegex.MatchString(name)
}

// ValidateServiceID ensures a service ID is a canonical UUID. This is used
// for validating service references.
func ValidateServiceID(serviceID string) bool {
	return ValidateIdentifier(serviceID)
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

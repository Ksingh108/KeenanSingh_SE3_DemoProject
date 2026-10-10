package validator

import (
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestValidateEmail(t *testing.T) {
	tests := []struct {
		email string
		valid bool
	}{
		{"test@example.com", true},
		{"user.name@domain.co.uk", true},
		{"invalid-email", false},
		{"@domain.com", false},
		{"user@", false},
		{"", false},
	}

	for _, tt := range tests {
		t.Run(tt.email, func(t *testing.T) {
			if ValidateEmail(tt.email) != tt.valid {
				t.Errorf("ValidateEmail(%s) = %v, want %v", tt.email, !tt.valid, tt.valid)
			}
		})
	}
}

func TestValidateDate(t *testing.T) {
	now := time.Now()
	futureDate := now.AddDate(0, 0, 1).Format("2006-01-02")
	pastDate := now.AddDate(0, 0, -1).Format("2006-01-02")

	tests := []struct {
		date  string
		valid bool
	}{
		{futureDate, true},
		{pastDate, false},
		{"2024-13-01", false},
		{"invalid-date", false},
		{"", false},
	}

	for _, tt := range tests {
		t.Run(tt.date, func(t *testing.T) {
			if ValidateDate(tt.date, now) != tt.valid {
				t.Errorf("ValidateDate(%s) = %v, want %v", tt.date, !tt.valid, tt.valid)
			}
		})
	}
}

// "Today" must follow the business time zone, not UTC. At 01:00 in Johannesburg
// on 7 October it is still 6 October in UTC, so 6 October is already in the past.
func TestValidateDate_UsesLocationOfNow(t *testing.T) {
	sast := time.FixedZone("SAST", 2*60*60)
	now := time.Date(2026, 10, 7, 1, 0, 0, 0, sast)

	tests := []struct {
		date  string
		valid bool
	}{
		{"2026-10-06", false}, // yesterday locally, though still "today" in UTC
		{"2026-10-07", true},  // today locally
		{"2026-10-08", true},
	}

	for _, tt := range tests {
		t.Run(tt.date, func(t *testing.T) {
			if got := ValidateDate(tt.date, now); got != tt.valid {
				t.Errorf("ValidateDate(%s, %v) = %v, want %v", tt.date, now, got, tt.valid)
			}
		})
	}
}

func TestValidateSlotNotPast(t *testing.T) {
	sast := time.FixedZone("SAST", 2*60*60)
	now := time.Date(2026, 10, 7, 10, 30, 0, 0, sast)

	tests := []struct {
		name     string
		date     string
		timeSlot string
		valid    bool
	}{
		{"earlier slot today", "2026-10-07", "09:00", false},
		{"slot that began a minute ago", "2026-10-07", "10:29", false},
		{"slot starting exactly now", "2026-10-07", "10:30", false},
		{"slot starting a minute from now", "2026-10-07", "10:31", true},
		{"later slot today", "2026-10-07", "14:00", true},
		{"any slot tomorrow", "2026-10-08", "09:00", true},
		{"any slot yesterday", "2026-10-06", "16:00", false},
		{"malformed date", "07-10-2026", "14:00", false},
		{"malformed slot", "2026-10-07", "2pm", false},
		{"invalid slot time", "2026-10-07", "25:00", false},
		{"empty", "", "", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := ValidateSlotNotPast(tt.date, tt.timeSlot, now); got != tt.valid {
				t.Errorf("ValidateSlotNotPast(%q, %q) = %v, want %v", tt.date, tt.timeSlot, got, tt.valid)
			}
		})
	}
}

func TestValidateTimeSlot(t *testing.T) {
	tests := []struct {
		timeSlot string
		valid    bool
	}{
		{"09:00", true},
		{"23:59", true},
		{"25:00", false},
		{"9:00", false},
		{"invalid", false},
		{"", false},
	}

	for _, tt := range tests {
		t.Run(tt.timeSlot, func(t *testing.T) {
			if ValidateTimeSlot(tt.timeSlot) != tt.valid {
				t.Errorf("ValidateTimeSlot(%s) = %v, want %v", tt.timeSlot, !tt.valid, tt.valid)
			}
		})
	}
}

func TestValidateBranchID(t *testing.T) {
	tests := []struct {
		branchID string
		valid    bool
	}{
		{"550e8400-e29b-41d4-a716-446655440001", true},
		{"branch-123", true},
		{"", false},
		{"invalid@branch", false},
	}

	for _, tt := range tests {
		t.Run(tt.branchID, func(t *testing.T) {
			if ValidateBranchID(tt.branchID) != tt.valid {
				t.Errorf("ValidateBranchID(%s) = %v, want %v", tt.branchID, !tt.valid, tt.valid)
			}
		})
	}
}

func TestValidatePhone(t *testing.T) {
	tests := []struct {
		name  string
		phone string
		valid bool
	}{
		{"international format", "+27 21 555 0100", true},
		{"hyphenated", "555-0123", true},
		{"local format", "021 555 0100", true},
		{"no separators", "0215550100", true},
		{"minimum digits", "1234567", true},
		{"maximum digits", "+123456789012345", true},
		{"too few digits", "123456", false},
		{"too many digits", "+1234567890123456", false},
		{"too long", strings.Repeat("1 ", 16), false},
		{"empty", "", false},
		{"letters only", "abcdefg", false},
		{"letters mixed in", "021 555 CALL", false},
		{"arbitrary text", "not a phone number", false},
		{"whitespace only", "          ", false},
		{"plus only", "+", false},
		{"plus in the middle", "021+5550100", false},
		{"double plus", "++27215550100", false},
		{"leading separator", "-0215550100", false},
		{"repeated separators", "021  555 0100", false},
		{"trailing separator", "021 555 0100 ", false},
		{"tab separator", "021\t5550100", false},
		{"newline", "0215550100\nINFO fake", false},
		{"semicolon", "021;555", false},
		{"markup", "<b>021</b>", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := ValidatePhone(tt.phone); got != tt.valid {
				t.Errorf("ValidatePhone(%q) = %v, want %v", tt.phone, got, tt.valid)
			}
		})
	}
}

func TestValidateCustomerName(t *testing.T) {
	tests := []struct {
		name     string
		custName string
		valid    bool
	}{
		{"simple", "Jane Smith", true},
		{"hyphenated", "Mary-Jane Watson", true},
		{"max length", strings.Repeat("a", 255), true},
		{"too long", strings.Repeat("a", 256), false},
		{"empty", "", false},
		{"apostrophe", "O'Brien", false},
		{"markup", "<script>", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := ValidateCustomerName(tt.custName); got != tt.valid {
				t.Errorf("ValidateCustomerName(%q) = %v, want %v", tt.custName, got, tt.valid)
			}
		})
	}
}

func TestValidateServiceID(t *testing.T) {
	tests := []struct {
		name      string
		serviceID string
		valid     bool
	}{
		{"uuid", "660e8400-e29b-41d4-a716-446655440001", true},
		{"max length", strings.Repeat("a", 50), true},
		{"too long", strings.Repeat("a", 51), false},
		{"empty", "", false},
		{"sql metacharacters", "abc'; --", false},
		{"slash", "a/b", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := ValidateServiceID(tt.serviceID); got != tt.valid {
				t.Errorf("ValidateServiceID(%q) = %v, want %v", tt.serviceID, got, tt.valid)
			}
		})
	}
}

func TestValidateBranchName(t *testing.T) {
	tests := []struct {
		name   string
		branch string
		valid  bool
	}{
		{"simple", "Diagon Alley Main", true},
		{"with punctuation", "St. John's & Co-op", true},
		{"max length", strings.Repeat("a", 255), true},
		{"too long", strings.Repeat("a", 256), false},
		{"empty", "", false},
		{"whitespace only", "   ", false},
		{"leading space", " Main", false},
		{"markup", "<script>alert(1)</script>", false},
		{"newline", "Main\nINFO forged", false},
		{"semicolon", "Main;DROP", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := ValidateBranchName(tt.branch); got != tt.valid {
				t.Errorf("ValidateBranchName(%q) = %v, want %v", tt.branch, got, tt.valid)
			}
		})
	}
}

func TestValidateCity(t *testing.T) {
	tests := []struct {
		name  string
		city  string
		valid bool
	}{
		{"simple", "Cape Town", true},
		{"hyphenated", "Stratford-upon-Avon", true},
		{"apostrophe", "Lil'wat", true},
		{"max length", strings.Repeat("a", 100), true},
		{"too long", strings.Repeat("a", 101), false},
		{"empty", "", false},
		{"whitespace only", "  ", false},
		{"digits", "City42", false},
		{"markup", "<b>", false},
		{"newline", "Cape\nTown", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := ValidateCity(tt.city); got != tt.valid {
				t.Errorf("ValidateCity(%q) = %v, want %v", tt.city, got, tt.valid)
			}
		})
	}
}

func TestValidateAddress(t *testing.T) {
	tests := []struct {
		name    string
		address string
		valid   bool
	}{
		{"simple", "123 Test St", true},
		{"with punctuation", "Unit 4/12, Main Rd. #2", true},
		{"max length", strings.Repeat("a", 500), true},
		{"too long", strings.Repeat("a", 501), false},
		{"empty", "", false},
		{"whitespace only", "   ", false},
		{"markup", "<img src=x onerror=alert(1)>", false},
		{"newline", "1 Main St\nINFO forged", false},
		{"semicolon", "1 Main;DROP", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := ValidateAddress(tt.address); got != tt.valid {
				t.Errorf("ValidateAddress(%q) = %v, want %v", tt.address, got, tt.valid)
			}
		})
	}
}

func TestValidateCapacity(t *testing.T) {
	tests := []struct {
		capacity int
		valid    bool
	}{
		{1, true},
		{15, true},
		{MaxBranchCapacity, true},
		{MaxBranchCapacity + 1, false},
		{0, false},
		{-1, false},
	}

	for _, tt := range tests {
		t.Run(strconv.Itoa(tt.capacity), func(t *testing.T) {
			if got := ValidateCapacity(tt.capacity); got != tt.valid {
				t.Errorf("ValidateCapacity(%d) = %v, want %v", tt.capacity, got, tt.valid)
			}
		})
	}
}

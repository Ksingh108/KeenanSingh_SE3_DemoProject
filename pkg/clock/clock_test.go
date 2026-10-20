package clock

import (
	"testing"
	"time"
)

func TestBusinessClock_ReportsBusinessTimeZone(t *testing.T) {
	now := NewBusinessClock().Now()

	name, offset := now.Zone()
	if offset != 2*60*60 {
		t.Errorf("Expected UTC+2 offset, got %d seconds (zone %s)", offset, name)
	}
	if got := now.Location().String(); got != BusinessLocation {
		t.Errorf("Expected location %q, got %q", BusinessLocation, got)
	}
}

func TestBusinessClock_IsCloseToSystemTime(t *testing.T) {
	before := time.Now()
	got := NewBusinessClock().Now()
	after := time.Now()

	if got.Before(before) || got.After(after) {
		t.Errorf("Expected %v to fall between %v and %v", got, before, after)
	}
}

func TestFixed(t *testing.T) {
	instant := time.Date(2026, 10, 6, 10, 30, 0, 0, time.UTC)

	if got := Fixed(instant).Now(); !got.Equal(instant) {
		t.Errorf("Expected %v, got %v", instant, got)
	}
}

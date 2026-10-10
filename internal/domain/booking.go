// Package domain contains the core business entities and domain logic for the
// Gringotts Bank Branch Booking System. This package defines the fundamental
// data structures and business rules that are independent of external concerns.
package domain

import (
	"time"
)

// Booking represents a customer's appointment to visit a bank branch.
// It contains all necessary information for scheduling and managing branch visits,
// including customer details, timing, and associated branch/service information.
//
// The struct uses both JSON and database tags for serialization and ORM mapping.
// Joined fields (Branch, Service) are populated when needed for API responses.
type Booking struct {
	ID           string    `json:"id" db:"id"`                       // Unique booking identifier (UUID)
	BranchID     string    `json:"branch_id" db:"branch_id"`         // Reference to the branch being visited
	ServiceID    string    `json:"service_id" db:"service_id"`       // Reference to the service being requested
	CustomerName string    `json:"customer_name" db:"customer_name"` // Full name of the customer
	Email        string    `json:"email" db:"email"`                 // Customer's email address (validated)
	Phone        string    `json:"phone" db:"phone"`                 // Customer's phone number
	Date         string    `json:"date" db:"date"`                   // Booking date in YYYY-MM-DD format
	TimeSlot     string    `json:"time_slot" db:"time_slot"`         // Time slot in HH:MM format (business hours)
	Status       string    `json:"status" db:"status"`               // Current booking status
	CreatedAt    time.Time `json:"created_at" db:"created_at"`       // Timestamp when booking was created

	// Joined fields - populated when retrieving booking details with related data
	Branch  *Branch  `json:"branch,omitempty"`  // Associated branch information
	Service *Service `json:"service,omitempty"` // Associated service information
}

// BookingStatus represents the possible states of a booking throughout its lifecycle.
// This type ensures type safety when working with booking statuses.
type BookingStatus string

// Booking status constants define the valid states for a booking.
// These statuses track the booking lifecycle from creation to completion.
const (
	StatusConfirmed BookingStatus = "confirmed" // Booking has been confirmed by the system
	StatusCancelled BookingStatus = "cancelled" // Booking has been cancelled by customer or admin
)

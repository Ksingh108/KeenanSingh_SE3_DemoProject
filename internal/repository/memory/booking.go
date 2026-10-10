package memory

import (
	"context"
	"fmt"
	"sort"
	"sync"
	"time"

	"github.com/GringottsBankBranchBooking/internal/domain"
	"github.com/google/uuid"
)

func getFutureDate() string {
	return time.Now().AddDate(0, 0, 30).Format("2006-01-02")
}

type BookingRepository struct {
	bookings map[string]*domain.Booking
	scenario string       // "empty", "partial", "full"
	mu       sync.RWMutex // Protects concurrent access during check-then-insert
}

func NewBookingRepository(scenario string) *BookingRepository {
	repo := &BookingRepository{
		bookings: make(map[string]*domain.Booking),
		scenario: scenario,
	}
	repo.seedData()
	return repo
}

func (r *BookingRepository) seedData() {
	switch r.scenario {
	case "empty":
		// No bookings - all slots available
		return
	case "partial":
		// Some slots booked
		futureDate := getFutureDate()
		r.bookings["1"] = &domain.Booking{
			ID:           "1",
			BranchID:     "550e8400-e29b-41d4-a716-446655440001",
			ServiceID:    "660e8400-e29b-41d4-a716-446655440002",
			CustomerName: "Harry Potter",
			Email:        "harry@hogwarts.edu",
			Phone:        "+44 7700 900123",
			Date:         futureDate,
			TimeSlot:     "09:00",
			Status:       string(domain.StatusConfirmed),
			CreatedAt:    time.Now(),
		}
		r.bookings["2"] = &domain.Booking{
			ID:           "2",
			BranchID:     "550e8400-e29b-41d4-a716-446655440001",
			ServiceID:    "660e8400-e29b-41d4-a716-446655440001",
			CustomerName: "Hermione Granger",
			Email:        "hermione@hogwarts.edu",
			Phone:        "+44 7700 900124",
			Date:         futureDate,
			TimeSlot:     "10:00",
			Status:       string(domain.StatusConfirmed),
			CreatedAt:    time.Now(),
		}
	case "full":
		// All business hour slots booked (9:00-17:00)
		futureDate := getFutureDate()
		slots := []string{"09:00", "10:00", "11:00", "12:00", "13:00", "14:00", "15:00", "16:00", "17:00"}
		for i, slot := range slots {
			id := uuid.New().String()
			r.bookings[id] = &domain.Booking{
				ID:           id,
				BranchID:     "550e8400-e29b-41d4-a716-446655440001",
				ServiceID:    "660e8400-e29b-41d4-a716-446655440002",
				CustomerName: fmt.Sprintf("Customer %d", i+1),
				Email:        fmt.Sprintf("customer%d@example.com", i+1),
				Phone:        fmt.Sprintf("+44 7700 90012%d", i+1),
				Date:         futureDate,
				TimeSlot:     slot,
				Status:       string(domain.StatusConfirmed),
				CreatedAt:    time.Now(),
			}
		}
	}
}

func (r *BookingRepository) Create(_ context.Context, booking *domain.Booking) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if booking.ID == "" {
		booking.ID = uuid.New().String()
	}
	booking.CreatedAt = time.Now()
	booking.Status = string(domain.StatusConfirmed)

	r.bookings[booking.ID] = booking
	return nil
}

// CreateWithCapacityCheck atomically checks capacity and creates a booking.
// Uses a write lock to ensure the capacity check and insert happen without race conditions.
func (r *BookingRepository) CreateWithCapacityCheck(_ context.Context, booking *domain.Booking, branchCapacity int) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	// Check current booking count for this specific time slot
	currentCount := 0
	for _, b := range r.bookings {
		if b.BranchID == booking.BranchID && b.Date == booking.Date && b.TimeSlot == booking.TimeSlot && b.Status != string(domain.StatusCancelled) {
			currentCount++
		}
	}

	// Enforce branch capacity limits
	if currentCount >= branchCapacity {
		return domain.ErrCapacityExceeded
	}

	// Create the booking
	if booking.ID == "" {
		booking.ID = uuid.New().String()
	}
	booking.CreatedAt = time.Now()
	booking.Status = string(domain.StatusConfirmed)

	r.bookings[booking.ID] = booking
	return nil
}

func (r *BookingRepository) GetAll(_ context.Context) ([]*domain.Booking, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	bookings := make([]*domain.Booking, 0, len(r.bookings))
	for _, booking := range r.bookings {
		bookings = append(bookings, booking)
	}
	// The repo is backed by a map, so ordering is explicit to keep pagination stable.
	sort.Slice(bookings, func(i, j int) bool {
		if bookings[i].CreatedAt.Equal(bookings[j].CreatedAt) {
			return bookings[i].ID < bookings[j].ID
		}
		return bookings[i].CreatedAt.Before(bookings[j].CreatedAt)
	})
	return bookings, nil
}

// Pagination is deterministic because the underlying store is a map and has no
// stable iteration order. We sort by creation time before slicing the result set.
func (r *BookingRepository) GetAllPaginated(_ context.Context, limit int, offset int) ([]*domain.Booking, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	all := make([]*domain.Booking, 0, len(r.bookings))
	for _, booking := range r.bookings {
		all = append(all, booking)
	}
	sort.Slice(all, func(i, j int) bool {
		if all[i].CreatedAt.Equal(all[j].CreatedAt) {
			return all[i].ID < all[j].ID
		}
		return all[i].CreatedAt.Before(all[j].CreatedAt)
	})

	// Handle pagination
	if offset >= len(all) {
		return []*domain.Booking{}, nil
	}

	end := offset + limit
	if end > len(all) {
		end = len(all)
	}

	return all[offset:end], nil
}

func (r *BookingRepository) GetByEmail(_ context.Context, email string) ([]*domain.Booking, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	var bookings []*domain.Booking
	for _, booking := range r.bookings {
		if booking.Email == email {
			bookings = append(bookings, booking)
		}
	}
	sort.Slice(bookings, func(i, j int) bool {
		if bookings[i].CreatedAt.Equal(bookings[j].CreatedAt) {
			return bookings[i].ID < bookings[j].ID
		}
		return bookings[i].CreatedAt.Before(bookings[j].CreatedAt)
	})
	return bookings, nil
}

func (r *BookingRepository) GetByEmailPaginated(_ context.Context, email string, limit int, offset int) ([]*domain.Booking, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	var all []*domain.Booking
	for _, booking := range r.bookings {
		if booking.Email == email {
			all = append(all, booking)
		}
	}
	sort.Slice(all, func(i, j int) bool {
		if all[i].CreatedAt.Equal(all[j].CreatedAt) {
			return all[i].ID < all[j].ID
		}
		return all[i].CreatedAt.Before(all[j].CreatedAt)
	})

	// Handle pagination
	if offset >= len(all) {
		return []*domain.Booking{}, nil
	}

	end := offset + limit
	if end > len(all) {
		end = len(all)
	}

	return all[offset:end], nil
}

func (r *BookingRepository) GetBookingCount(_ context.Context, branchID, date, timeSlot string) (int, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	count := 0
	for _, booking := range r.bookings {
		if booking.BranchID == branchID && booking.Date == date && booking.TimeSlot == timeSlot && booking.Status != string(domain.StatusCancelled) {
			count++
		}
	}
	return count, nil
}

func (r *BookingRepository) GetBookedSlots(_ context.Context, branchID, date string) (map[string]int, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	slots := make(map[string]int)
	for _, booking := range r.bookings {
		if booking.BranchID == branchID && booking.Date == date && booking.Status != string(domain.StatusCancelled) {
			slots[booking.TimeSlot]++
		}
	}
	return slots, nil
}

func (r *BookingRepository) Cancel(_ context.Context, bookingID string) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	booking, exists := r.bookings[bookingID]
	if !exists {
		return domain.ErrBookingNotFound
	}
	booking.Status = string(domain.StatusCancelled)
	return nil
}

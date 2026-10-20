package memory

import (
	"context"
	"fmt"
	"sort"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/gringottsbankbranchbooking/internal/domain"
)

func getFutureDate() string {
	return time.Now().AddDate(0, 0, 30).Format("2006-01-02")
}

type BookingRepository struct {
	bookings    map[string]*domain.Booking
	idempotency map[string]idempotencyRecord
	scenario    string // "empty", "partial", "full"
	capacity    int
	mu          sync.RWMutex // Protects concurrent access during check-then-insert
}

type idempotencyRecord struct {
	fingerprint string
	bookingID   string
}

func NewBookingRepository(scenario string, capacity ...int) *BookingRepository {
	branchCapacity := 15
	if len(capacity) > 0 {
		branchCapacity = capacity[0]
	}
	repo := &BookingRepository{
		bookings:    make(map[string]*domain.Booking),
		idempotency: make(map[string]idempotencyRecord),
		scenario:    scenario,
		capacity:    branchCapacity,
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
		r.bookings["770e8400-e29b-41d4-a716-446655440001"] = &domain.Booking{
			ID:           "770e8400-e29b-41d4-a716-446655440001",
			BranchID:     "550e8400-e29b-41d4-a716-446655440001",
			ServiceID:    "660e8400-e29b-41d4-a716-446655440002",
			CustomerName: "Harry Potter",
			Email:        "harry@hogwarts.edu",
			Phone:        "+27 82 123 4567",
			Date:         futureDate,
			TimeSlot:     "09:00",
			Status:       string(domain.StatusConfirmed),
			CreatedAt:    time.Now(),
		}
		r.bookings["770e8400-e29b-41d4-a716-446655440002"] = &domain.Booking{
			ID:           "770e8400-e29b-41d4-a716-446655440002",
			BranchID:     "550e8400-e29b-41d4-a716-446655440001",
			ServiceID:    "660e8400-e29b-41d4-a716-446655440001",
			CustomerName: "Hermione Granger",
			Email:        "hermione@hogwarts.edu",
			Phone:        "+27 82 123 4568",
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
				Phone:        fmt.Sprintf("+27 82 123 45%02d", i+7),
				Date:         futureDate,
				TimeSlot:     slot,
				Status:       string(domain.StatusConfirmed),
				CreatedAt:    time.Now(),
			}
		}
	}
}

// CreateWithCapacityCheck atomically checks capacity and creates a booking.
// Uses a write lock to ensure the capacity check and insert happen without race conditions.
func (r *BookingRepository) CreateWithCapacityCheck(_ context.Context, booking *domain.Booking) error {
	return r.createWithCapacityCheck(booking, "", "")
}

func (r *BookingRepository) CreateWithCapacityCheckIdempotent(_ context.Context, booking *domain.Booking, key, fingerprint string) error {
	return r.createWithCapacityCheck(booking, key, fingerprint)
}

func (r *BookingRepository) GetByIdempotencyKey(_ context.Context, key string) (*domain.Booking, string, bool, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	record, exists := r.idempotency[key]
	if !exists {
		return nil, "", false, nil
	}
	booking := *r.bookings[record.bookingID]
	return &booking, record.fingerprint, true, nil
}

func (r *BookingRepository) createWithCapacityCheck(booking *domain.Booking, key, fingerprint string) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if key != "" {
		if record, exists := r.idempotency[key]; exists {
			if record.fingerprint != fingerprint {
				return domain.ErrIdempotencyKeyReused
			}
			if existing := r.bookings[record.bookingID]; existing.Status == string(domain.StatusCancelled) {
				return domain.ErrIdempotentBookingCancelled
			}
			*booking = *r.bookings[record.bookingID]
			return nil
		}
	}

	// Check current booking count for this specific time slot
	currentCount := 0
	for _, b := range r.bookings {
		if b.BranchID == booking.BranchID && b.Date == booking.Date && b.TimeSlot == booking.TimeSlot && b.Status != string(domain.StatusCancelled) {
			currentCount++
		}
	}

	// Enforce branch capacity limits
	if currentCount >= r.capacity {
		return domain.ErrCapacityExceeded
	}

	// Create the booking
	if booking.ID == "" {
		booking.ID = uuid.New().String()
	}
	booking.CreatedAt = time.Now()
	booking.Status = string(domain.StatusConfirmed)

	r.bookings[booking.ID] = booking
	if key != "" {
		r.idempotency[key] = idempotencyRecord{fingerprint: fingerprint, bookingID: booking.ID}
	}
	return nil
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

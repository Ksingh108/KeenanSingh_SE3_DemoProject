//go:build integration

package postgres

import "testing"

func TestSchema_EnforcesDomainConstraints(t *testing.T) {
	t.Run("booking service must be offered at branch", func(t *testing.T) {
		resetDB(t)

		_, err := testDB.Exec(`
			INSERT INTO bookings (
				branch_id, service_id, customer_name, email, phone, date, time_slot
			) VALUES ($1, $2, 'Bad Actor', 'bad@example.com', '+27 00 000 0000', '2030-01-02', '09:00')
		`, seededBranchID2, seededServiceID)
		if err == nil {
			t.Fatal("expected booking with a service not offered at the branch to be rejected")
		}
	})

	t.Run("booking status must be valid", func(t *testing.T) {
		resetDB(t)

		_, err := testDB.Exec(`
			INSERT INTO bookings (
				branch_id, service_id, customer_name, email, phone, date, time_slot, status
			) VALUES ($1, $2, 'Bad Actor', 'bad@example.com', '+27 00 000 0000', '2030-01-02', '09:00', 'pending')
		`, seededBranchID, seededServiceID)
		if err == nil {
			t.Fatal("expected invalid booking status to be rejected by the database")
		}
	})

	t.Run("branch capacity must be positive", func(t *testing.T) {
		resetDB(t)

		_, err := testDB.Exec(`
			INSERT INTO branches (name, address, city, phone, hours, capacity)
			VALUES ('Invalid Branch', '1 Invalid Road', 'London', '+44 20 7946 9999', 'Mon-Fri 9:00-17:00', 0)
		`)
		if err == nil {
			t.Fatal("expected zero-capacity branch to be rejected by the database")
		}
	})

	t.Run("default status is confirmed", func(t *testing.T) {
		resetDB(t)

		var status string
		err := testDB.QueryRow(`
			INSERT INTO bookings (
				branch_id, service_id, customer_name, email, phone, date, time_slot
			) VALUES ($1, $2, 'Jane Smith', 'jane@example.com', '+27 21 555 0100', '2030-01-03', '10:00')
			RETURNING status
		`, seededBranchID, seededServiceID).Scan(&status)
		if err != nil {
			t.Fatalf("insert booking without status: %v", err)
		}
		if status != "confirmed" {
			t.Fatalf("expected default status confirmed, got %q", status)
		}
	})
}

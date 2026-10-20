package domain

import "time"

// Audited admin actions. The value is stored in the audit log, so changing an
// existing constant would orphan historical rows.
const (
	AuditActionBookingCancel = "booking.cancel"
	AuditActionBranchCreate  = "branch.create"
	AuditActionBranchDelete  = "branch.delete"
)

// Audited target types, i.e. the kind of record an action was performed on.
const (
	AuditTargetBooking = "booking"
	AuditTargetBranch  = "branch"
)

// AuditEntry records who performed a privileged action, what they did and to
// which record, supporting non-repudiation for regulated environments.
// Details carries action-specific context, such as how many bookings were
// removed when a branch was deleted.
type AuditEntry struct {
	ID         string    `json:"id"`
	AdminID    string    `json:"admin_id"`
	Action     string    `json:"action"`
	TargetType string    `json:"target_type"`
	TargetID   string    `json:"target_id"`
	Details    string    `json:"details,omitempty"`
	RequestID  string    `json:"request_id,omitempty"`
	CreatedAt  time.Time `json:"created_at"`
}

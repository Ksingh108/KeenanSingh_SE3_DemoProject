package domain

import "time"

type Admin struct {
	ID           string    `json:"id" db:"id"`
	Username     string    `json:"username" db:"username"`
	PasswordHash string    `json:"-" db:"password_hash"`
	CreatedAt    time.Time `json:"created_at" db:"created_at"`
}

// AdminSession is a persisted admin login. TokenHash is the hex-encoded SHA-256
// digest of the bearer token held in the admin's cookie; the raw token is never
// stored, so a leaked session table cannot be replayed as live sessions.
type AdminSession struct {
	TokenHash string    `json:"-"`
	AdminID   string    `json:"admin_id"`
	ExpiresAt time.Time `json:"expires_at"`
}

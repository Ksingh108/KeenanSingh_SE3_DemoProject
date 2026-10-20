package domain

import "time"

type Branch struct {
	ID        string     `json:"id" db:"id"`
	Name      string     `json:"name" db:"name"`
	Address   string     `json:"address" db:"address"`
	City      string     `json:"city" db:"city"`
	Phone     string     `json:"phone" db:"phone"`
	Hours     string     `json:"hours" db:"hours"`
	Capacity  int        `json:"capacity" db:"capacity"`
	Services  []*Service `json:"services,omitempty"`
	CreatedAt time.Time  `json:"created_at" db:"created_at"`
}

type Service struct {
	ID          string    `json:"id" db:"id"`
	Name        string    `json:"name" db:"name"`
	Description string    `json:"description" db:"description"`
	CreatedAt   time.Time `json:"created_at" db:"created_at"`
}

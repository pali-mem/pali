package domain

import "time"

// Tenant is the persisted tenant record.
type Tenant struct {
	ID        string
	Name      string
	CreatedAt time.Time
}

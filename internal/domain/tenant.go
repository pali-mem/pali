package domain

import "time"

type Tenant struct {
	ID        string
	Name      string
	CreatedAt time.Time
}

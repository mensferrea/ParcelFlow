package domain

import (
	"time"

	"github.com/google/uuid"
)

type Role string

const (
	RoleCustomer Role = "CUSTOMER"
	RoleOperator Role = "OPERATOR"
)

type User struct {
	ID           uuid.UUID `json:"id"`
	Email        string    `json:"email"`
	PasswordHash string    `json:"-"`
	Role         Role      `json:"role"`
	CreatedAt    time.Time `json:"created_at"`
}

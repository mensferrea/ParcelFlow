package domain

import (
	"time"

	"github.com/google/uuid"
)

type PaymentStatus string

const (
	PaymentStatusPending   PaymentStatus = "PENDING"
	PaymentStatusSucceeded PaymentStatus = "SUCCEEDED"
	PaymentStatusFailed    PaymentStatus = "FAILED"
	PaymentStatusCancelled PaymentStatus = "CANCELLED"
)

type Payment struct {
	ID                uuid.UUID     `json:"id"`
	ShipmentID        uuid.UUID     `json:"shipment_id"`
	ExternalPaymentID string        `json:"external_payment_id"`
	IdempotencyKey    string        `json:"idempotency_key"`
	AmountCents       int64         `json:"amount_cents"`
	Currency          string        `json:"currency"`
	Status            PaymentStatus `json:"status"`
	CreatedAt         time.Time     `json:"created_at"`
	UpdatedAt         time.Time     `json:"updated_at"`
}

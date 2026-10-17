package domain

import (
	"time"

	"github.com/google/uuid"
)

type ShipmentStatus string

const (
	StatusCreated           ShipmentStatus = "CREATED"
	StatusWaitingForPayment ShipmentStatus = "WAITING_FOR_PAYMENT"
	StatusPaid              ShipmentStatus = "PAID"
	StatusAccepted          ShipmentStatus = "ACCEPTED"
	StatusInTransit         ShipmentStatus = "IN_TRANSIT"
	StatusOutForDelivery    ShipmentStatus = "OUT_FOR_DELIVERY"
	StatusDelivered         ShipmentStatus = "DELIVERED"
	StatusPaymentFailed     ShipmentStatus = "PAYMENT_FAILED"
	StatusCancelled         ShipmentStatus = "CANCELLED"
)

type DeliveryType string

const (
	DeliveryTypeStandard DeliveryType = "STANDARD"
	DeliveryTypeExpress  DeliveryType = "EXPRESS"
	DeliveryTypeFreight  DeliveryType = "FREIGHT"
)

type Shipment struct {
	ID               uuid.UUID      `json:"id"`
	TrackingNumber   string         `json:"tracking_number"`
	CustomerID       uuid.UUID      `json:"customer_id"`
	Status           ShipmentStatus `json:"status"`
	SenderName       string         `json:"sender_name"`
	SenderPhone      string         `json:"sender_phone"`
	SenderAddress    string         `json:"sender_address"`
	RecipientName    string         `json:"recipient_name"`
	RecipientPhone   string         `json:"recipient_phone"`
	RecipientAddress string         `json:"recipient_address"`
	OriginCity       string         `json:"origin_city"`
	DestinationCity  string         `json:"destination_city"`
	WeightKg         float64        `json:"weight_kg"`
	LengthCm         float64        `json:"length_cm"`
	WidthCm          float64        `json:"width_cm"`
	HeightCm         float64        `json:"height_cm"`
	DeliveryType     DeliveryType   `json:"delivery_type"`
	PriceCents       int64          `json:"price_cents"`
	Currency         string         `json:"currency"`
	IdempotencyKey   *string        `json:"idempotency_key,omitempty"`
	CreatedAt        time.Time      `json:"created_at"`
	UpdatedAt        time.Time      `json:"updated_at"`
}

type CreateShipmentInput struct {
	CustomerID       uuid.UUID    `json:"-"`
	SenderName       string       `json:"sender_name"`
	SenderPhone      string       `json:"sender_phone"`
	SenderAddress    string       `json:"sender_address"`
	RecipientName    string       `json:"recipient_name"`
	RecipientPhone   string       `json:"recipient_phone"`
	RecipientAddress string       `json:"recipient_address"`
	OriginCity       string       `json:"origin_city"`
	DestinationCity  string       `json:"destination_city"`
	WeightKg         float64      `json:"weight_kg"`
	LengthCm         float64      `json:"length_cm"`
	WidthCm          float64      `json:"width_cm"`
	HeightCm         float64      `json:"height_cm"`
	DeliveryType     DeliveryType `json:"delivery_type"`
	IdempotencyKey   *string      `json:"idempotency_key,omitempty"`
}

type ListShipmentsFilter struct {
	CustomerID *uuid.UUID
	Status     *ShipmentStatus
	StartDate  *time.Time
	EndDate    *time.Time
	Page       int
	PageSize   int
}

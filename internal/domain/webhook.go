package domain

import (
	"time"

	"github.com/google/uuid"
)

type WebhookSubscription struct {
	ID         uuid.UUID `json:"id"`
	CustomerID uuid.UUID `json:"customer_id"`
	TargetURL  string    `json:"target_url"`
	Events     []string  `json:"events"`
	Secret     string    `json:"secret"`
	Active     bool      `json:"active"`
	CreatedAt  time.Time `json:"created_at"`
}

type WebhookDelivery struct {
	ID             uuid.UUID `json:"id"`
	SubscriptionID uuid.UUID `json:"subscription_id"`
	EventID        string    `json:"event_id"`
	StatusCode     *int      `json:"status_code,omitempty"`
	Attempt        int       `json:"attempt"`
	RequestBody    string    `json:"request_body"`
	ResponseBody   *string   `json:"response_body,omitempty"`
	Success        bool      `json:"success"`
	ErrorMessage   *string   `json:"error_message,omitempty"`
	CreatedAt      time.Time `json:"created_at"`
}

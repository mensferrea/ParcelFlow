package domain

import (
	"encoding/json"
	"time"

	"github.com/google/uuid"
)

type OutboxStatus string

const (
	OutboxStatusPending   OutboxStatus = "PENDING"
	OutboxStatusPublished OutboxStatus = "PUBLISHED"
	OutboxStatusFailed    OutboxStatus = "FAILED"
)

type OutboxEvent struct {
	ID          uuid.UUID       `json:"id"`
	EventType   string          `json:"event_type"`
	AggregateID uuid.UUID       `json:"aggregate_id"`
	Payload     json.RawMessage `json:"payload"`
	Status      OutboxStatus    `json:"status"`
	RetryCount  int             `json:"retry_count"`
	LastError   *string         `json:"last_error,omitempty"`
	CreatedAt   time.Time       `json:"created_at"`
	PublishedAt *time.Time      `json:"published_at,omitempty"`
}

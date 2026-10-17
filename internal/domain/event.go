package domain

import (
	"time"

	"github.com/google/uuid"
)

type ShipmentEvent struct {
	ID          uuid.UUID      `json:"id"`
	ShipmentID  uuid.UUID      `json:"shipment_id"`
	EventID     string         `json:"event_id"`
	OldStatus   ShipmentStatus `json:"old_status"`
	NewStatus   ShipmentStatus `json:"new_status"`
	Source      string         `json:"source"`
	Description string         `json:"description"`
	CreatedAt   time.Time      `json:"created_at"`
}

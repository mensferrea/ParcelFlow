package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	"ParcelFlow/internal/domain"
)

type Repository struct {
	db *sql.DB
}

func NewRepository(db *sql.DB) *Repository {
	return &Repository{db: db}
}

func (r *Repository) DB() *sql.DB {
	return r.db
}

func (r *Repository) CreateUser(ctx context.Context, user *domain.User) error {
	query := `
		INSERT INTO users (id, email, password_hash, role, created_at)
		VALUES ($1, $2, $3, $4, $5)
	`
	_, err := r.db.ExecContext(ctx, query, user.ID, user.Email, user.PasswordHash, string(user.Role), user.CreatedAt)
	return err
}

func (r *Repository) GetUserByEmail(ctx context.Context, email string) (*domain.User, error) {
	query := `
		SELECT id, email, password_hash, role, created_at
		FROM users
		WHERE email = $1
	`
	var user domain.User
	var role string
	err := r.db.QueryRowContext(ctx, query, email).Scan(
		&user.ID,
		&user.Email,
		&user.PasswordHash,
		&role,
		&user.CreatedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, domain.ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	user.Role = domain.Role(role)
	return &user, nil
}

func (r *Repository) GetShipmentByIdempotencyKey(ctx context.Context, key string) (*domain.Shipment, error) {
	query := `
		SELECT id, tracking_number, customer_id, status, sender_name, sender_phone, sender_address,
		       recipient_name, recipient_phone, recipient_address, origin_city, destination_city,
		       weight_kg, length_cm, width_cm, height_cm, delivery_type, price_cents, currency,
		       idempotency_key, created_at, updated_at
		FROM shipments
		WHERE idempotency_key = $1
	`
	return r.scanShipment(r.db.QueryRowContext(ctx, query, key))
}

func (r *Repository) CreateShipmentTx(ctx context.Context, tx *sql.Tx, s *domain.Shipment, event *domain.ShipmentEvent, outbox *domain.OutboxEvent) error {
	shipmentQuery := `
		INSERT INTO shipments (
			id, tracking_number, customer_id, status, sender_name, sender_phone, sender_address,
			recipient_name, recipient_phone, recipient_address, origin_city, destination_city,
			weight_kg, length_cm, width_cm, height_cm, delivery_type, price_cents, currency,
			idempotency_key, created_at, updated_at
		) VALUES (
			$1, $2, $3, $4, $5, $6, $7,
			$8, $9, $10, $11, $12,
			$13, $14, $15, $16, $17, $18, $19,
			$20, $21, $22
		)
	`
	_, err := tx.ExecContext(ctx, shipmentQuery,
		s.ID, s.TrackingNumber, s.CustomerID, string(s.Status), s.SenderName, s.SenderPhone, s.SenderAddress,
		s.RecipientName, s.RecipientPhone, s.RecipientAddress, s.OriginCity, s.DestinationCity,
		s.WeightKg, s.LengthCm, s.WidthCm, s.HeightCm, string(s.DeliveryType), s.PriceCents, s.Currency,
		s.IdempotencyKey, s.CreatedAt, s.UpdatedAt,
	)
	if err != nil {
		return fmt.Errorf("insert shipment: %w", err)
	}

	eventQuery := `
		INSERT INTO shipment_events (id, shipment_id, event_id, old_status, new_status, source, description, created_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
	`
	_, err = tx.ExecContext(ctx, eventQuery,
		event.ID, event.ShipmentID, event.EventID, string(event.OldStatus), string(event.NewStatus),
		event.Source, event.Description, event.CreatedAt,
	)
	if err != nil {
		return fmt.Errorf("insert event: %w", err)
	}

	outboxQuery := `
		INSERT INTO outbox_events (id, event_type, aggregate_id, payload, status, retry_count, created_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
	`
	_, err = tx.ExecContext(ctx, outboxQuery,
		outbox.ID, outbox.EventType, outbox.AggregateID, outbox.Payload, string(outbox.Status),
		outbox.RetryCount, outbox.CreatedAt,
	)
	if err != nil {
		return fmt.Errorf("insert outbox: %w", err)
	}

	return nil
}

func (r *Repository) GetShipmentByID(ctx context.Context, id uuid.UUID) (*domain.Shipment, error) {
	query := `
		SELECT id, tracking_number, customer_id, status, sender_name, sender_phone, sender_address,
		       recipient_name, recipient_phone, recipient_address, origin_city, destination_city,
		       weight_kg, length_cm, width_cm, height_cm, delivery_type, price_cents, currency,
		       idempotency_key, created_at, updated_at
		FROM shipments
		WHERE id = $1
	`
	return r.scanShipment(r.db.QueryRowContext(ctx, query, id))
}

func (r *Repository) ListShipments(ctx context.Context, filter domain.ListShipmentsFilter) ([]domain.Shipment, int64, error) {
	whereClauses := []string{"1=1"}
	args := []any{}
	argIdx := 1

	if filter.CustomerID != nil {
		whereClauses = append(whereClauses, fmt.Sprintf("customer_id = $%d", argIdx))
		args = append(args, *filter.CustomerID)
		argIdx++
	}

	if filter.Status != nil {
		whereClauses = append(whereClauses, fmt.Sprintf("status = $%d", argIdx))
		args = append(args, string(*filter.Status))
		argIdx++
	}

	if filter.StartDate != nil {
		whereClauses = append(whereClauses, fmt.Sprintf("created_at >= $%d", argIdx))
		args = append(args, *filter.StartDate)
		argIdx++
	}

	if filter.EndDate != nil {
		whereClauses = append(whereClauses, fmt.Sprintf("created_at <= $%d", argIdx))
		args = append(args, *filter.EndDate)
		argIdx++
	}

	whereSQL := strings.Join(whereClauses, " AND ")

	countQuery := fmt.Sprintf("SELECT COUNT(*) FROM shipments WHERE %s", whereSQL)
	var total int64
	if err := r.db.QueryRowContext(ctx, countQuery, args...).Scan(&total); err != nil {
		return nil, 0, err
	}

	if filter.Page < 1 {
		filter.Page = 1
	}
	if filter.PageSize < 1 || filter.PageSize > 100 {
		filter.PageSize = 20
	}
	offset := (filter.Page - 1) * filter.PageSize

	dataQuery := fmt.Sprintf(`
		SELECT id, tracking_number, customer_id, status, sender_name, sender_phone, sender_address,
		       recipient_name, recipient_phone, recipient_address, origin_city, destination_city,
		       weight_kg, length_cm, width_cm, height_cm, delivery_type, price_cents, currency,
		       idempotency_key, created_at, updated_at
		FROM shipments
		WHERE %s
		ORDER BY created_at DESC
		LIMIT $%d OFFSET $%d
	`, whereSQL, argIdx, argIdx+1)

	args = append(args, filter.PageSize, offset)

	rows, err := r.db.QueryContext(ctx, dataQuery, args...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	var shipments []domain.Shipment
	for rows.Next() {
		var s domain.Shipment
		var status, deliveryType string
		err := rows.Scan(
			&s.ID, &s.TrackingNumber, &s.CustomerID, &status, &s.SenderName, &s.SenderPhone, &s.SenderAddress,
			&s.RecipientName, &s.RecipientPhone, &s.RecipientAddress, &s.OriginCity, &s.DestinationCity,
			&s.WeightKg, &s.LengthCm, &s.WidthCm, &s.HeightCm, &deliveryType, &s.PriceCents, &s.Currency,
			&s.IdempotencyKey, &s.CreatedAt, &s.UpdatedAt,
		)
		if err != nil {
			return nil, 0, err
		}
		s.Status = domain.ShipmentStatus(status)
		s.DeliveryType = domain.DeliveryType(deliveryType)
		shipments = append(shipments, s)
	}

	return shipments, total, nil
}

func (r *Repository) UpdateStatusTx(ctx context.Context, tx *sql.Tx, shipmentID uuid.UUID, nextStatus domain.ShipmentStatus, event *domain.ShipmentEvent, outbox *domain.OutboxEvent) error {
	updateQuery := `
		UPDATE shipments
		SET status = $1, updated_at = $2
		WHERE id = $3
	`
	_, err := tx.ExecContext(ctx, updateQuery, string(nextStatus), time.Now(), shipmentID)
	if err != nil {
		return fmt.Errorf("update shipment status: %w", err)
	}

	eventQuery := `
		INSERT INTO shipment_events (id, shipment_id, event_id, old_status, new_status, source, description, created_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
	`
	_, err = tx.ExecContext(ctx, eventQuery,
		event.ID, event.ShipmentID, event.EventID, string(event.OldStatus), string(event.NewStatus),
		event.Source, event.Description, event.CreatedAt,
	)
	if err != nil {
		return fmt.Errorf("insert event: %w", err)
	}

	outboxQuery := `
		INSERT INTO outbox_events (id, event_type, aggregate_id, payload, status, retry_count, created_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
	`
	_, err = tx.ExecContext(ctx, outboxQuery,
		outbox.ID, outbox.EventType, outbox.AggregateID, outbox.Payload, string(outbox.Status),
		outbox.RetryCount, outbox.CreatedAt,
	)
	if err != nil {
		return fmt.Errorf("insert outbox: %w", err)
	}

	return nil
}

func (r *Repository) GetShipmentEvents(ctx context.Context, shipmentID uuid.UUID) ([]domain.ShipmentEvent, error) {
	query := `
		SELECT id, shipment_id, event_id, old_status, new_status, source, description, created_at
		FROM shipment_events
		WHERE shipment_id = $1
		ORDER BY created_at ASC
	`
	rows, err := r.db.QueryContext(ctx, query, shipmentID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var events []domain.ShipmentEvent
	for rows.Next() {
		var e domain.ShipmentEvent
		var oldStatus, newStatus string
		err := rows.Scan(
			&e.ID, &e.ShipmentID, &e.EventID, &oldStatus, &newStatus,
			&e.Source, &e.Description, &e.CreatedAt,
		)
		if err != nil {
			return nil, err
		}
		e.OldStatus = domain.ShipmentStatus(oldStatus)
		e.NewStatus = domain.ShipmentStatus(newStatus)
		events = append(events, e)
	}

	return events, nil
}

func (r *Repository) CreatePaymentTx(ctx context.Context, tx *sql.Tx, payment *domain.Payment) error {
	query := `
		INSERT INTO payments (id, shipment_id, external_payment_id, idempotency_key, amount_cents, currency, status, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
	`
	_, err := tx.ExecContext(ctx, query,
		payment.ID, payment.ShipmentID, payment.ExternalPaymentID, payment.IdempotencyKey,
		payment.AmountCents, payment.Currency, string(payment.Status), payment.CreatedAt, payment.UpdatedAt,
	)
	return err
}

func (r *Repository) GetPaymentByID(ctx context.Context, id uuid.UUID) (*domain.Payment, error) {
	query := `
		SELECT id, shipment_id, external_payment_id, idempotency_key, amount_cents, currency, status, created_at, updated_at
		FROM payments
		WHERE id = $1
	`
	var p domain.Payment
	var status string
	err := r.db.QueryRowContext(ctx, query, id).Scan(
		&p.ID, &p.ShipmentID, &p.ExternalPaymentID, &p.IdempotencyKey,
		&p.AmountCents, &p.Currency, &status, &p.CreatedAt, &p.UpdatedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, domain.ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	p.Status = domain.PaymentStatus(status)
	return &p, nil
}

func (r *Repository) GetPaymentByIdempotencyKey(ctx context.Context, key string) (*domain.Payment, error) {
	query := `
		SELECT id, shipment_id, external_payment_id, idempotency_key, amount_cents, currency, status, created_at, updated_at
		FROM payments
		WHERE idempotency_key = $1
	`
	var p domain.Payment
	var status string
	err := r.db.QueryRowContext(ctx, query, key).Scan(
		&p.ID, &p.ShipmentID, &p.ExternalPaymentID, &p.IdempotencyKey,
		&p.AmountCents, &p.Currency, &status, &p.CreatedAt, &p.UpdatedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, domain.ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	p.Status = domain.PaymentStatus(status)
	return &p, nil
}

func (r *Repository) MarkWebhookProcessedTx(ctx context.Context, tx *sql.Tx, eventID, provider string) (bool, error) {
	query := `
		INSERT INTO processed_webhooks (event_id, provider, processed_at)
		VALUES ($1, $2, $3)
		ON CONFLICT (event_id) DO NOTHING
	`
	res, err := tx.ExecContext(ctx, query, eventID, provider, time.Now())
	if err != nil {
		return false, err
	}
	rows, err := res.RowsAffected()
	if err != nil {
		return false, err
	}
	return rows > 0, nil
}

func (r *Repository) UpdatePaymentStatusTx(ctx context.Context, tx *sql.Tx, externalID string, status domain.PaymentStatus) error {
	query := `
		UPDATE payments
		SET status = $1, updated_at = $2
		WHERE external_payment_id = $3
	`
	_, err := tx.ExecContext(ctx, query, string(status), time.Now(), externalID)
	return err
}

func (r *Repository) CreateWebhookSubscription(ctx context.Context, sub *domain.WebhookSubscription) error {
	query := `
		INSERT INTO webhook_subscriptions (id, customer_id, target_url, events, secret, active, created_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
	`
	_, err := r.db.ExecContext(ctx, query,
		sub.ID, sub.CustomerID, sub.TargetURL, sub.Events, sub.Secret, sub.Active, sub.CreatedAt,
	)
	return err
}

func (r *Repository) GetActiveSubscriptionsForEvent(ctx context.Context, eventType string) ([]domain.WebhookSubscription, error) {
	query := `
		SELECT id, customer_id, target_url, events, secret, active, created_at
		FROM webhook_subscriptions
		WHERE active = TRUE AND $1 = ANY(events)
	`
	rows, err := r.db.QueryContext(ctx, query, eventType)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var subs []domain.WebhookSubscription
	for rows.Next() {
		var s domain.WebhookSubscription
		var events []string
		err := rows.Scan(
			&s.ID, &s.CustomerID, &s.TargetURL, &events, &s.Secret, &s.Active, &s.CreatedAt,
		)
		if err != nil {
			return nil, err
		}
		s.Events = events
		subs = append(subs, s)
	}

	return subs, nil
}

func (r *Repository) LogWebhookDelivery(ctx context.Context, d *domain.WebhookDelivery) error {
	query := `
		INSERT INTO webhook_deliveries (id, subscription_id, event_id, status_code, attempt, request_body, response_body, success, error_message, created_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
	`
	_, err := r.db.ExecContext(ctx, query,
		d.ID, d.SubscriptionID, d.EventID, d.StatusCode, d.Attempt, d.RequestBody,
		d.ResponseBody, d.Success, d.ErrorMessage, d.CreatedAt,
	)
	return err
}

func (r *Repository) FetchPendingOutboxEvents(ctx context.Context, limit int) ([]domain.OutboxEvent, error) {
	query := `
		SELECT id, event_type, aggregate_id, payload, status, retry_count, last_error, created_at, published_at
		FROM outbox_events
		WHERE status = 'PENDING'
		ORDER BY created_at ASC
		LIMIT $1
		FOR UPDATE SKIP LOCKED
	`
	rows, err := r.db.QueryContext(ctx, query, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var events []domain.OutboxEvent
	for rows.Next() {
		var e domain.OutboxEvent
		var status string
		var rawPayload []byte
		err := rows.Scan(
			&e.ID, &e.EventType, &e.AggregateID, &rawPayload, &status,
			&e.RetryCount, &e.LastError, &e.CreatedAt, &e.PublishedAt,
		)
		if err != nil {
			return nil, err
		}
		e.Status = domain.OutboxStatus(status)
		e.Payload = json.RawMessage(rawPayload)
		events = append(events, e)
	}

	return events, nil
}

func (r *Repository) MarkOutboxEventPublished(ctx context.Context, id uuid.UUID) error {
	query := `
		UPDATE outbox_events
		SET status = 'PUBLISHED', published_at = $1
		WHERE id = $2
	`
	_, err := r.db.ExecContext(ctx, query, time.Now(), id)
	return err
}

func (r *Repository) MarkOutboxEventFailed(ctx context.Context, id uuid.UUID, errMsg string) error {
	query := `
		UPDATE outbox_events
		SET retry_count = retry_count + 1, last_error = $1
		WHERE id = $2
	`
	_, err := r.db.ExecContext(ctx, query, errMsg, id)
	return err
}

func (r *Repository) scanShipment(row *sql.Row) (*domain.Shipment, error) {
	var s domain.Shipment
	var status, deliveryType string
	err := row.Scan(
		&s.ID, &s.TrackingNumber, &s.CustomerID, &status, &s.SenderName, &s.SenderPhone, &s.SenderAddress,
		&s.RecipientName, &s.RecipientPhone, &s.RecipientAddress, &s.OriginCity, &s.DestinationCity,
		&s.WeightKg, &s.LengthCm, &s.WidthCm, &s.HeightCm, &deliveryType, &s.PriceCents, &s.Currency,
		&s.IdempotencyKey, &s.CreatedAt, &s.UpdatedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, domain.ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	s.Status = domain.ShipmentStatus(status)
	s.DeliveryType = domain.DeliveryType(deliveryType)
	return &s, nil
}

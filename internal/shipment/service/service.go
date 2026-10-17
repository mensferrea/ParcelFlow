package service

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math/big"
	"strings"
	"time"

	"github.com/google/uuid"

	"ParcelFlow/internal/domain"
	paymentclient "ParcelFlow/internal/payment/client"
	"ParcelFlow/internal/payment/simulator"
	pricingclient "ParcelFlow/internal/pricing/client"
	"ParcelFlow/internal/shipment/repository"
	"ParcelFlow/pkg/redis"
)

type ShipmentService struct {
	repo           *repository.Repository
	pricingClient  *pricingclient.PricingClient
	paymentClient  *paymentclient.PaymentClient
	redisClient    *redis.Client
	webhookBaseURL string
}

func NewShipmentService(
	repo *repository.Repository,
	pricingClient *pricingclient.PricingClient,
	paymentClient *paymentclient.PaymentClient,
	redisClient *redis.Client,
	webhookBaseURL string,
) *ShipmentService {
	return &ShipmentService{
		repo:           repo,
		pricingClient:  pricingClient,
		paymentClient:  paymentClient,
		redisClient:    redisClient,
		webhookBaseURL: webhookBaseURL,
	}
}

func (s *ShipmentService) CreateShipment(ctx context.Context, input domain.CreateShipmentInput) (*domain.Shipment, error) {
	if input.IdempotencyKey != nil && *input.IdempotencyKey != "" {
		existing, err := s.repo.GetShipmentByIdempotencyKey(ctx, *input.IdempotencyKey)
		if err == nil && existing != nil {
			return existing, nil
		}
	}

	rate, err := s.pricingClient.CalculateRate(ctx, input)
	if err != nil {
		return nil, fmt.Errorf("calculate rate: %w", err)
	}

	trackingNumber, err := s.generateTrackingNumber()
	if err != nil {
		return nil, fmt.Errorf("generate tracking number: %w", err)
	}

	now := time.Now()
	shipment := &domain.Shipment{
		ID:               uuid.New(),
		TrackingNumber:   trackingNumber,
		CustomerID:       input.CustomerID,
		Status:           domain.StatusCreated,
		SenderName:       input.SenderName,
		SenderPhone:      input.SenderPhone,
		SenderAddress:    input.SenderAddress,
		RecipientName:    input.RecipientName,
		RecipientPhone:   input.RecipientPhone,
		RecipientAddress: input.RecipientAddress,
		OriginCity:       input.OriginCity,
		DestinationCity:  input.DestinationCity,
		WeightKg:         input.WeightKg,
		LengthCm:         input.LengthCm,
		WidthCm:          input.WidthCm,
		HeightCm:         input.HeightCm,
		DeliveryType:     input.DeliveryType,
		PriceCents:       rate.GetPriceCents(),
		Currency:         rate.GetCurrency(),
		IdempotencyKey:   input.IdempotencyKey,
		CreatedAt:        now,
		UpdatedAt:        now,
	}

	event := &domain.ShipmentEvent{
		ID:          uuid.New(),
		ShipmentID:  shipment.ID,
		EventID:     fmt.Sprintf("evt_%s", uuid.New().String()),
		OldStatus:   "",
		NewStatus:   domain.StatusCreated,
		Source:      "CUSTOMER_API",
		Description: "Shipment created",
		CreatedAt:   now,
	}

	eventPayload, err := json.Marshal(map[string]any{
		"event_id":        event.EventID,
		"shipment_id":     shipment.ID,
		"customer_id":     shipment.CustomerID,
		"tracking_number": shipment.TrackingNumber,
		"old_status":      event.OldStatus,
		"new_status":      event.NewStatus,
		"source":          event.Source,
		"timestamp":       event.CreatedAt,
	})
	if err != nil {
		return nil, err
	}

	outbox := &domain.OutboxEvent{
		ID:          uuid.New(),
		EventType:   "shipment.created",
		AggregateID: shipment.ID,
		Payload:     eventPayload,
		Status:      domain.OutboxStatusPending,
		RetryCount:  0,
		CreatedAt:   now,
	}

	tx, err := s.repo.DB().BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("begin transaction: %w", err)
	}
	defer tx.Rollback()

	if err := s.repo.CreateShipmentTx(ctx, tx, shipment, event, outbox); err != nil {
		return nil, err
	}

	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit transaction: %w", err)
	}

	return shipment, nil
}

func (s *ShipmentService) GetShipmentByID(ctx context.Context, id uuid.UUID, requestingUserID uuid.UUID, role domain.Role) (*domain.Shipment, error) {
	cacheKey := fmt.Sprintf("shipment:%s", id.String())
	if s.redisClient != nil {
		if cached, err := s.redisClient.Get(ctx, cacheKey); err == nil && cached != "" {
			var shipment domain.Shipment
			if err := json.Unmarshal([]byte(cached), &shipment); err == nil {
				if role != domain.RoleOperator && shipment.CustomerID != requestingUserID {
					return nil, domain.ErrForbidden
				}
				return &shipment, nil
			}
		}
	}

	shipment, err := s.repo.GetShipmentByID(ctx, id)
	if err != nil {
		return nil, err
	}

	if role != domain.RoleOperator && shipment.CustomerID != requestingUserID {
		return nil, domain.ErrForbidden
	}

	if s.redisClient != nil {
		data, err := json.Marshal(shipment)
		if err == nil {
			_ = s.redisClient.Set(ctx, cacheKey, data, 10*time.Minute)
		}
	}

	return shipment, nil
}

func (s *ShipmentService) ListShipments(ctx context.Context, filter domain.ListShipmentsFilter) ([]domain.Shipment, int64, error) {
	return s.repo.ListShipments(ctx, filter)
}

func (s *ShipmentService) CancelShipment(ctx context.Context, id uuid.UUID, requestingUserID uuid.UUID, role domain.Role) error {
	shipment, err := s.repo.GetShipmentByID(ctx, id)
	if err != nil {
		return err
	}

	if role != domain.RoleOperator && shipment.CustomerID != requestingUserID {
		return domain.ErrForbidden
	}

	if !domain.CanCancel(shipment.Status) {
		return domain.ErrCannotCancel
	}

	return s.transitionStatus(ctx, shipment, domain.StatusCancelled, "USER_ACTION", "Shipment cancelled by user")
}

func (s *ShipmentService) GetShipmentEvents(ctx context.Context, id uuid.UUID, requestingUserID uuid.UUID, role domain.Role) ([]domain.ShipmentEvent, error) {
	shipment, err := s.repo.GetShipmentByID(ctx, id)
	if err != nil {
		return nil, err
	}

	if role != domain.RoleOperator && shipment.CustomerID != requestingUserID {
		return nil, domain.ErrForbidden
	}

	return s.repo.GetShipmentEvents(ctx, id)
}

func (s *ShipmentService) InitiatePayment(ctx context.Context, shipmentID uuid.UUID, idempotencyKey string, requestingUserID uuid.UUID) (*domain.Payment, error) {
	shipment, err := s.repo.GetShipmentByID(ctx, shipmentID)
	if err != nil {
		return nil, err
	}

	if shipment.CustomerID != requestingUserID {
		return nil, domain.ErrForbidden
	}

	if existing, err := s.repo.GetPaymentByIdempotencyKey(ctx, idempotencyKey); err == nil && existing != nil {
		return existing, nil
	}

	if shipment.Status != domain.StatusCreated && shipment.Status != domain.StatusPaymentFailed {
		return nil, fmt.Errorf("cannot initiate payment for shipment in status %s: %w", shipment.Status, domain.ErrInvalidStateTransition)
	}

	webhookURL := fmt.Sprintf("%s/api/v1/webhooks/payments", s.webhookBaseURL)
	paymentResp, err := s.paymentClient.CreatePayment(ctx, simulator.CreatePaymentRequest{
		ShipmentID:     shipment.ID.String(),
		IdempotencyKey: idempotencyKey,
		AmountCents:    shipment.PriceCents,
		Currency:       shipment.Currency,
		WebhookURL:     webhookURL,
	})
	if err != nil {
		return nil, fmt.Errorf("create external payment: %w", err)
	}

	now := time.Now()
	payment := &domain.Payment{
		ID:                uuid.New(),
		ShipmentID:        shipment.ID,
		ExternalPaymentID: paymentResp.ID,
		IdempotencyKey:    idempotencyKey,
		AmountCents:       shipment.PriceCents,
		Currency:          shipment.Currency,
		Status:            domain.PaymentStatusPending,
		CreatedAt:         now,
		UpdatedAt:         now,
	}

	event := &domain.ShipmentEvent{
		ID:          uuid.New(),
		ShipmentID:  shipment.ID,
		EventID:     fmt.Sprintf("evt_%s", uuid.New().String()),
		OldStatus:   shipment.Status,
		NewStatus:   domain.StatusWaitingForPayment,
		Source:      "PAYMENT_INITIATE",
		Description: "Payment initiated",
		CreatedAt:   now,
	}

	eventPayload, err := json.Marshal(map[string]any{
		"event_id":        event.EventID,
		"shipment_id":     shipment.ID,
		"customer_id":     shipment.CustomerID,
		"tracking_number": shipment.TrackingNumber,
		"old_status":      event.OldStatus,
		"new_status":      event.NewStatus,
		"source":          event.Source,
		"timestamp":       event.CreatedAt,
	})
	if err != nil {
		return nil, err
	}

	outbox := &domain.OutboxEvent{
		ID:          uuid.New(),
		EventType:   "shipment.waiting_for_payment",
		AggregateID: shipment.ID,
		Payload:     eventPayload,
		Status:      domain.OutboxStatusPending,
		RetryCount:  0,
		CreatedAt:   now,
	}

	tx, err := s.repo.DB().BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	if err := s.repo.CreatePaymentTx(ctx, tx, payment); err != nil {
		return nil, fmt.Errorf("insert payment: %w", err)
	}

	if err := s.repo.UpdateStatusTx(ctx, tx, shipment.ID, domain.StatusWaitingForPayment, event, outbox); err != nil {
		return nil, fmt.Errorf("update status on payment: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit payment tx: %w", err)
	}

	if s.redisClient != nil {
		_ = s.redisClient.Delete(ctx, fmt.Sprintf("shipment:%s", shipment.ID.String()))
	}

	return payment, nil
}

func (s *ShipmentService) GetPaymentByID(ctx context.Context, id uuid.UUID, requestingUserID uuid.UUID, role domain.Role) (*domain.Payment, error) {
	payment, err := s.repo.GetPaymentByID(ctx, id)
	if err != nil {
		return nil, err
	}

	if role != domain.RoleOperator {
		shipment, err := s.repo.GetShipmentByID(ctx, payment.ShipmentID)
		if err != nil {
			return nil, err
		}
		if shipment.CustomerID != requestingUserID {
			return nil, domain.ErrForbidden
		}
	}

	return payment, nil
}

func (s *ShipmentService) ProcessPaymentWebhook(ctx context.Context, payload simulator.WebhookPayload) error {
	tx, err := s.repo.DB().BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	inserted, err := s.repo.MarkWebhookProcessedTx(ctx, tx, payload.EventID, "PAYMENT_SIMULATOR")
	if err != nil {
		return fmt.Errorf("mark webhook processed: %w", err)
	}
	if !inserted {
		return nil
	}

	shipmentID, err := uuid.Parse(payload.ShipmentID)
	if err != nil {
		return fmt.Errorf("parse shipment id: %w", err)
	}

	shipment, err := s.repo.GetShipmentByID(ctx, shipmentID)
	if err != nil {
		return fmt.Errorf("get shipment: %w", err)
	}

	var targetStatus domain.ShipmentStatus
	var paymentStatus domain.PaymentStatus
	if payload.Status == "SUCCEEDED" {
		targetStatus = domain.StatusPaid
		paymentStatus = domain.PaymentStatusSucceeded
	} else {
		targetStatus = domain.StatusPaymentFailed
		paymentStatus = domain.PaymentStatusFailed
	}

	if err := domain.ValidateTransition(shipment.Status, targetStatus); err != nil {
		return nil
	}

	now := time.Now()
	event := &domain.ShipmentEvent{
		ID:          uuid.New(),
		ShipmentID:  shipment.ID,
		EventID:     payload.EventID,
		OldStatus:   shipment.Status,
		NewStatus:   targetStatus,
		Source:      "PAYMENT_GATEWAY",
		Description: fmt.Sprintf("Payment %s: status %s", payload.PaymentID, payload.Status),
		CreatedAt:   now,
	}

	eventPayload, err := json.Marshal(map[string]any{
		"event_id":        event.EventID,
		"shipment_id":     shipment.ID,
		"customer_id":     shipment.CustomerID,
		"tracking_number": shipment.TrackingNumber,
		"old_status":      event.OldStatus,
		"new_status":      event.NewStatus,
		"source":          event.Source,
		"timestamp":       event.CreatedAt,
	})
	if err != nil {
		return err
	}

	outbox := &domain.OutboxEvent{
		ID:          uuid.New(),
		EventType:   fmt.Sprintf("shipment.%s", targetStatus),
		AggregateID: shipment.ID,
		Payload:     eventPayload,
		Status:      domain.OutboxStatusPending,
		RetryCount:  0,
		CreatedAt:   now,
	}

	if err := s.repo.UpdatePaymentStatusTx(ctx, tx, payload.PaymentID, paymentStatus); err != nil {
		return fmt.Errorf("update payment status: %w", err)
	}

	if err := s.repo.UpdateStatusTx(ctx, tx, shipment.ID, targetStatus, event, outbox); err != nil {
		return fmt.Errorf("update status from webhook: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit webhook tx: %w", err)
	}

	if s.redisClient != nil {
		_ = s.redisClient.Delete(ctx, fmt.Sprintf("shipment:%s", shipment.ID.String()))
	}

	return nil
}

func (s *ShipmentService) RegisterTrackingEvent(ctx context.Context, shipmentID uuid.UUID, nextStatus domain.ShipmentStatus, description, source string) error {
	shipment, err := s.repo.GetShipmentByID(ctx, shipmentID)
	if err != nil {
		return err
	}

	return s.transitionStatus(ctx, shipment, nextStatus, source, description)
}

func (s *ShipmentService) RegisterWebhookSubscription(ctx context.Context, sub *domain.WebhookSubscription) error {
	sub.ID = uuid.New()
	sub.CreatedAt = time.Now()
	sub.Active = true
	return s.repo.CreateWebhookSubscription(ctx, sub)
}

func (s *ShipmentService) transitionStatus(ctx context.Context, shipment *domain.Shipment, next domain.ShipmentStatus, source, desc string) error {
	if err := domain.ValidateTransition(shipment.Status, next); err != nil {
		return err
	}

	now := time.Now()
	event := &domain.ShipmentEvent{
		ID:          uuid.New(),
		ShipmentID:  shipment.ID,
		EventID:     fmt.Sprintf("evt_%s", uuid.New().String()),
		OldStatus:   shipment.Status,
		NewStatus:   next,
		Source:      source,
		Description: desc,
		CreatedAt:   now,
	}

	eventPayload, err := json.Marshal(map[string]any{
		"event_id":        event.EventID,
		"shipment_id":     shipment.ID,
		"customer_id":     shipment.CustomerID,
		"tracking_number": shipment.TrackingNumber,
		"old_status":      event.OldStatus,
		"new_status":      event.NewStatus,
		"source":          event.Source,
		"timestamp":       event.CreatedAt,
	})
	if err != nil {
		return err
	}

	outbox := &domain.OutboxEvent{
		ID:          uuid.New(),
		EventType:   fmt.Sprintf("shipment.%s", next),
		AggregateID: shipment.ID,
		Payload:     eventPayload,
		Status:      domain.OutboxStatusPending,
		RetryCount:  0,
		CreatedAt:   now,
	}

	tx, err := s.repo.DB().BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if err := s.repo.UpdateStatusTx(ctx, tx, shipment.ID, next, event, outbox); err != nil {
		return err
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit transition: %w", err)
	}

	if s.redisClient != nil {
		_ = s.redisClient.Delete(ctx, fmt.Sprintf("shipment:%s", shipment.ID.String()))
	}

	return nil
}

func (s *ShipmentService) generateTrackingNumber() (string, error) {
	bytes := make([]byte, 4)
	if _, err := rand.Read(bytes); err != nil {
		return "", err
	}
	num, _ := rand.Int(rand.Reader, big.NewInt(900000))
	return fmt.Sprintf("PF-%s-%06d", strings.ToUpper(hex.EncodeToString(bytes)), num.Int64()+100000), nil
}

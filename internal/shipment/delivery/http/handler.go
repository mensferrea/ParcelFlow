package http

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"

	"ParcelFlow/internal/auth"
	"ParcelFlow/internal/domain"
	"ParcelFlow/internal/payment/simulator"
	"ParcelFlow/internal/shipment/service"
	"ParcelFlow/pkg/httphelper"
	"ParcelFlow/pkg/redis"
)

type contextKey string

const userContextKey contextKey = "currentUser"

type Handler struct {
	svc          *service.ShipmentService
	tokenManager *auth.TokenManager
	redisClient  *redis.Client
}

func NewHandler(svc *service.ShipmentService, tm *auth.TokenManager, rc *redis.Client) *Handler {
	return &Handler{
		svc:          svc,
		tokenManager: tm,
		redisClient:  rc,
	}
}

func (h *Handler) Routes() http.Handler {
	mux := http.NewServeMux()

	mux.HandleFunc("POST /api/v1/shipments", h.authMiddleware(h.rateLimitMiddleware(h.handleCreateShipment)))
	mux.HandleFunc("GET /api/v1/shipments/{id}", h.authMiddleware(h.handleGetShipment))
	mux.HandleFunc("GET /api/v1/shipments", h.authMiddleware(h.handleListShipments))
	mux.HandleFunc("POST /api/v1/shipments/{id}/cancel", h.authMiddleware(h.handleCancelShipment))
	mux.HandleFunc("GET /api/v1/shipments/{id}/events", h.authMiddleware(h.handleGetShipmentEvents))
	mux.HandleFunc("POST /api/v1/shipments/{id}/payments", h.authMiddleware(h.rateLimitMiddleware(h.handleInitiatePayment)))
	mux.HandleFunc("GET /api/v1/payments/{id}", h.authMiddleware(h.handleGetPayment))
	mux.HandleFunc("POST /api/v1/webhooks/payments", h.handlePaymentWebhook)
	mux.HandleFunc("POST /api/v1/shipments/{id}/tracking-events", h.authRoleMiddleware(domain.RoleOperator, h.handleRegisterTrackingEvent))
	mux.HandleFunc("GET /api/v1/operator/shipments", h.authRoleMiddleware(domain.RoleOperator, h.handleOperatorListShipments))
	mux.HandleFunc("POST /api/v1/webhooks/subscriptions", h.authMiddleware(h.handleRegisterWebhookSubscription))

	return mux
}

func (h *Handler) authMiddleware(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		authHeader := r.Header.Get("Authorization")
		if !strings.HasPrefix(authHeader, "Bearer ") {
			httphelper.WriteError(w, http.StatusUnauthorized, "missing or invalid authorization header")
			return
		}

		tokenStr := strings.TrimPrefix(authHeader, "Bearer ")
		claims, err := h.tokenManager.ValidateToken(tokenStr)
		if err != nil {
			httphelper.WriteError(w, http.StatusUnauthorized, "invalid token")
			return
		}

		ctx := context.WithValue(r.Context(), userContextKey, claims)
		next(w, r.WithContext(ctx))
	}
}

func (h *Handler) authRoleMiddleware(requiredRole domain.Role, next http.HandlerFunc) http.HandlerFunc {
	return h.authMiddleware(func(w http.ResponseWriter, r *http.Request) {
		claims, ok := r.Context().Value(userContextKey).(*auth.Claims)
		if !ok || claims.Role != requiredRole {
			httphelper.WriteError(w, http.StatusForbidden, "insufficient permissions")
			return
		}
		next(w, r)
	})
}

func (h *Handler) rateLimitMiddleware(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if h.redisClient != nil {
			claims, ok := r.Context().Value(userContextKey).(*auth.Claims)
			key := "ratelimit:" + r.RemoteAddr
			if ok && claims != nil {
				key = "ratelimit:" + claims.UserID.String()
			}
			allowed, err := h.redisClient.AllowRateLimit(r.Context(), key, 60, time.Minute)
			if err == nil && !allowed {
				httphelper.WriteError(w, http.StatusTooManyRequests, "rate limit exceeded")
				return
			}
		}
		next(w, r)
	}
}

func (h *Handler) handleCreateShipment(w http.ResponseWriter, r *http.Request) {
	claims := r.Context().Value(userContextKey).(*auth.Claims)

	var input domain.CreateShipmentInput
	if err := httphelper.ReadJSON(r, &input); err != nil {
		httphelper.WriteError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	input.CustomerID = claims.UserID
	if idempotencyKey := r.Header.Get("X-Idempotency-Key"); idempotencyKey != "" {
		input.IdempotencyKey = &idempotencyKey
	}

	if input.SenderName == "" || input.RecipientName == "" || input.OriginCity == "" || input.DestinationCity == "" {
		httphelper.WriteError(w, http.StatusBadRequest, "missing required fields")
		return
	}

	if input.DeliveryType == "" {
		input.DeliveryType = domain.DeliveryTypeStandard
	}

	shipment, err := h.svc.CreateShipment(r.Context(), input)
	if err != nil {
		httphelper.WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}

	httphelper.WriteJSON(w, http.StatusCreated, shipment)
}

func (h *Handler) handleGetShipment(w http.ResponseWriter, r *http.Request) {
	claims := r.Context().Value(userContextKey).(*auth.Claims)
	id, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		httphelper.WriteError(w, http.StatusBadRequest, "invalid shipment id")
		return
	}

	shipment, err := h.svc.GetShipmentByID(r.Context(), id, claims.UserID, claims.Role)
	if errors.Is(err, domain.ErrNotFound) {
		httphelper.WriteError(w, http.StatusNotFound, "shipment not found")
		return
	}
	if errors.Is(err, domain.ErrForbidden) {
		httphelper.WriteError(w, http.StatusForbidden, "access denied")
		return
	}
	if err != nil {
		httphelper.WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}

	httphelper.WriteJSON(w, http.StatusOK, shipment)
}

func (h *Handler) handleListShipments(w http.ResponseWriter, r *http.Request) {
	claims := r.Context().Value(userContextKey).(*auth.Claims)

	page, _ := strconv.Atoi(r.URL.Query().Get("page"))
	pageSize, _ := strconv.Atoi(r.URL.Query().Get("page_size"))

	var statusFilter *domain.ShipmentStatus
	if st := r.URL.Query().Get("status"); st != "" {
		s := domain.ShipmentStatus(st)
		statusFilter = &s
	}

	var startDateFilter *time.Time
	if sd := r.URL.Query().Get("start_date"); sd != "" {
		if t, err := time.Parse(time.RFC3339, sd); err == nil {
			startDateFilter = &t
		}
	}

	var endDateFilter *time.Time
	if ed := r.URL.Query().Get("end_date"); ed != "" {
		if t, err := time.Parse(time.RFC3339, ed); err == nil {
			endDateFilter = &t
		}
	}

	filter := domain.ListShipmentsFilter{
		CustomerID: &claims.UserID,
		Status:     statusFilter,
		StartDate:  startDateFilter,
		EndDate:    endDateFilter,
		Page:       page,
		PageSize:   pageSize,
	}

	shipments, total, err := h.svc.ListShipments(r.Context(), filter)
	if err != nil {
		httphelper.WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}

	httphelper.WriteJSON(w, http.StatusOK, httphelper.PaginatedResponse[domain.Shipment]{
		Items:      shipments,
		TotalCount: total,
		Page:       filter.Page,
		PageSize:   filter.PageSize,
	})
}

func (h *Handler) handleOperatorListShipments(w http.ResponseWriter, r *http.Request) {
	page, _ := strconv.Atoi(r.URL.Query().Get("page"))
	pageSize, _ := strconv.Atoi(r.URL.Query().Get("page_size"))

	var customerFilter *uuid.UUID
	if cid := r.URL.Query().Get("customer_id"); cid != "" {
		if parsed, err := uuid.Parse(cid); err == nil {
			customerFilter = &parsed
		}
	}

	var statusFilter *domain.ShipmentStatus
	if st := r.URL.Query().Get("status"); st != "" {
		s := domain.ShipmentStatus(st)
		statusFilter = &s
	}

	filter := domain.ListShipmentsFilter{
		CustomerID: customerFilter,
		Status:     statusFilter,
		Page:       page,
		PageSize:   pageSize,
	}

	shipments, total, err := h.svc.ListShipments(r.Context(), filter)
	if err != nil {
		httphelper.WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}

	httphelper.WriteJSON(w, http.StatusOK, httphelper.PaginatedResponse[domain.Shipment]{
		Items:      shipments,
		TotalCount: total,
		Page:       filter.Page,
		PageSize:   filter.PageSize,
	})
}

func (h *Handler) handleCancelShipment(w http.ResponseWriter, r *http.Request) {
	claims := r.Context().Value(userContextKey).(*auth.Claims)
	id, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		httphelper.WriteError(w, http.StatusBadRequest, "invalid shipment id")
		return
	}

	err = h.svc.CancelShipment(r.Context(), id, claims.UserID, claims.Role)
	if errors.Is(err, domain.ErrNotFound) {
		httphelper.WriteError(w, http.StatusNotFound, "shipment not found")
		return
	}
	if errors.Is(err, domain.ErrForbidden) {
		httphelper.WriteError(w, http.StatusForbidden, "access denied")
		return
	}
	if errors.Is(err, domain.ErrCannotCancel) {
		httphelper.WriteError(w, http.StatusConflict, err.Error())
		return
	}
	if err != nil {
		httphelper.WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}

	httphelper.WriteJSON(w, http.StatusOK, map[string]string{"status": "CANCELLED"})
}

func (h *Handler) handleGetShipmentEvents(w http.ResponseWriter, r *http.Request) {
	claims := r.Context().Value(userContextKey).(*auth.Claims)
	id, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		httphelper.WriteError(w, http.StatusBadRequest, "invalid shipment id")
		return
	}

	events, err := h.svc.GetShipmentEvents(r.Context(), id, claims.UserID, claims.Role)
	if errors.Is(err, domain.ErrNotFound) {
		httphelper.WriteError(w, http.StatusNotFound, "shipment not found")
		return
	}
	if errors.Is(err, domain.ErrForbidden) {
		httphelper.WriteError(w, http.StatusForbidden, "access denied")
		return
	}
	if err != nil {
		httphelper.WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}

	httphelper.WriteJSON(w, http.StatusOK, events)
}

func (h *Handler) handleInitiatePayment(w http.ResponseWriter, r *http.Request) {
	claims := r.Context().Value(userContextKey).(*auth.Claims)
	id, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		httphelper.WriteError(w, http.StatusBadRequest, "invalid shipment id")
		return
	}

	idempotencyKey := r.Header.Get("X-Idempotency-Key")
	if idempotencyKey == "" {
		idempotencyKey = "pay_req_" + uuid.New().String()
	}

	payment, err := h.svc.InitiatePayment(r.Context(), id, idempotencyKey, claims.UserID)
	if errors.Is(err, domain.ErrNotFound) {
		httphelper.WriteError(w, http.StatusNotFound, "shipment not found")
		return
	}
	if errors.Is(err, domain.ErrForbidden) {
		httphelper.WriteError(w, http.StatusForbidden, "access denied")
		return
	}
	if errors.Is(err, domain.ErrInvalidStateTransition) {
		httphelper.WriteError(w, http.StatusConflict, err.Error())
		return
	}
	if err != nil {
		httphelper.WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}

	httphelper.WriteJSON(w, http.StatusCreated, payment)
}

func (h *Handler) handleGetPayment(w http.ResponseWriter, r *http.Request) {
	claims := r.Context().Value(userContextKey).(*auth.Claims)
	id, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		httphelper.WriteError(w, http.StatusBadRequest, "invalid payment id")
		return
	}

	payment, err := h.svc.GetPaymentByID(r.Context(), id, claims.UserID, claims.Role)
	if errors.Is(err, domain.ErrNotFound) {
		httphelper.WriteError(w, http.StatusNotFound, "payment not found")
		return
	}
	if errors.Is(err, domain.ErrForbidden) {
		httphelper.WriteError(w, http.StatusForbidden, "access denied")
		return
	}
	if err != nil {
		httphelper.WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}

	httphelper.WriteJSON(w, http.StatusOK, payment)
}

func (h *Handler) handlePaymentWebhook(w http.ResponseWriter, r *http.Request) {
	var payload simulator.WebhookPayload
	if err := httphelper.ReadJSON(r, &payload); err != nil {
		httphelper.WriteError(w, http.StatusBadRequest, "invalid payload")
		return
	}

	if payload.EventID == "" || payload.PaymentID == "" || payload.ShipmentID == "" {
		httphelper.WriteError(w, http.StatusBadRequest, "missing required webhook fields")
		return
	}

	if err := h.svc.ProcessPaymentWebhook(r.Context(), payload); err != nil {
		httphelper.WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}

	httphelper.WriteJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

type TrackingEventRequest struct {
	Status      domain.ShipmentStatus `json:"status"`
	Description string                `json:"description"`
}

func (h *Handler) handleRegisterTrackingEvent(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		httphelper.WriteError(w, http.StatusBadRequest, "invalid shipment id")
		return
	}

	var req TrackingEventRequest
	if err := httphelper.ReadJSON(r, &req); err != nil {
		httphelper.WriteError(w, http.StatusBadRequest, "invalid payload")
		return
	}

	if req.Status == "" {
		httphelper.WriteError(w, http.StatusBadRequest, "status is required")
		return
	}

	err = h.svc.RegisterTrackingEvent(r.Context(), id, req.Status, req.Description, "OPERATOR_PORTAL")
	if errors.Is(err, domain.ErrNotFound) {
		httphelper.WriteError(w, http.StatusNotFound, "shipment not found")
		return
	}
	if errors.Is(err, domain.ErrInvalidStateTransition) {
		httphelper.WriteError(w, http.StatusConflict, "invalid state transition")
		return
	}
	if err != nil {
		httphelper.WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}

	httphelper.WriteJSON(w, http.StatusOK, map[string]string{"status": string(req.Status)})
}

func (h *Handler) handleRegisterWebhookSubscription(w http.ResponseWriter, r *http.Request) {
	claims := r.Context().Value(userContextKey).(*auth.Claims)

	var sub domain.WebhookSubscription
	if err := httphelper.ReadJSON(r, &sub); err != nil {
		httphelper.WriteError(w, http.StatusBadRequest, "invalid payload")
		return
	}

	if sub.TargetURL == "" || len(sub.Events) == 0 {
		httphelper.WriteError(w, http.StatusBadRequest, "target_url and events are required")
		return
	}

	sub.CustomerID = claims.UserID
	if sub.Secret == "" {
		sub.Secret = uuid.New().String()
	}

	if err := h.svc.RegisterWebhookSubscription(r.Context(), &sub); err != nil {
		httphelper.WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}

	httphelper.WriteJSON(w, http.StatusCreated, sub)
}

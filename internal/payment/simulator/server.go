package simulator

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/google/uuid"

	"ParcelFlow/pkg/httphelper"
)

type PaymentRecord struct {
	ID             string    `json:"id"`
	ShipmentID     string    `json:"shipment_id"`
	IdempotencyKey string    `json:"idempotency_key"`
	AmountCents    int64     `json:"amount_cents"`
	Currency       string    `json:"currency"`
	Status         string    `json:"status"`
	WebhookURL     string    `json:"webhook_url"`
	CreatedAt      time.Time `json:"created_at"`
	UpdatedAt      time.Time `json:"updated_at"`
}

type CreatePaymentRequest struct {
	ShipmentID     string `json:"shipment_id"`
	IdempotencyKey string `json:"idempotency_key"`
	AmountCents    int64  `json:"amount_cents"`
	Currency       string `json:"currency"`
	WebhookURL     string `json:"webhook_url"`
}

type WebhookPayload struct {
	EventID     string    `json:"event_id"`
	PaymentID   string    `json:"payment_id"`
	ShipmentID  string    `json:"shipment_id"`
	Status      string    `json:"status"`
	AmountCents int64     `json:"amount_cents"`
	Currency    string    `json:"currency"`
	Timestamp   time.Time `json:"timestamp"`
}

type Simulator struct {
	mu             sync.RWMutex
	payments       map[string]*PaymentRecord
	idempotencyMap map[string]*PaymentRecord
	httpClient     *http.Client
}

func NewSimulator() *Simulator {
	return &Simulator{
		payments:       make(map[string]*PaymentRecord),
		idempotencyMap: make(map[string]*PaymentRecord),
		httpClient:     &http.Client{Timeout: 5 * time.Second},
	}
}

func (s *Simulator) Routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /payments", s.handleCreatePayment)
	mux.HandleFunc("GET /payments/{id}", s.handleGetPayment)
	mux.HandleFunc("POST /payments/{id}/confirm", s.handleConfirmPayment)
	mux.HandleFunc("POST /payments/{id}/reject", s.handleRejectPayment)
	return mux
}

func (s *Simulator) handleCreatePayment(w http.ResponseWriter, r *http.Request) {
	if timeoutHeader := r.Header.Get("X-Simulate-Timeout"); timeoutHeader != "" {
		if ms, err := strconv.Atoi(timeoutHeader); err == nil && ms > 0 {
			time.Sleep(time.Duration(ms) * time.Millisecond)
		}
	}

	if errorHeader := r.Header.Get("X-Simulate-Error"); errorHeader != "" {
		if code, err := strconv.Atoi(errorHeader); err == nil && code >= 400 {
			httphelper.WriteError(w, code, "simulated payment gateway failure")
			return
		}
	}

	var req CreatePaymentRequest
	if err := httphelper.ReadJSON(r, &req); err != nil {
		httphelper.WriteError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if req.IdempotencyKey == "" || req.ShipmentID == "" || req.AmountCents <= 0 {
		httphelper.WriteError(w, http.StatusBadRequest, "missing required fields")
		return
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if existing, ok := s.idempotencyMap[req.IdempotencyKey]; ok {
		httphelper.WriteJSON(w, http.StatusOK, existing)
		return
	}

	now := time.Now()
	payment := &PaymentRecord{
		ID:             uuid.New().String(),
		ShipmentID:     req.ShipmentID,
		IdempotencyKey: req.IdempotencyKey,
		AmountCents:    req.AmountCents,
		Currency:       req.Currency,
		Status:         "PENDING",
		WebhookURL:     req.WebhookURL,
		CreatedAt:      now,
		UpdatedAt:      now,
	}

	s.payments[payment.ID] = payment
	s.idempotencyMap[payment.IdempotencyKey] = payment

	httphelper.WriteJSON(w, http.StatusCreated, payment)
}

func (s *Simulator) handleGetPayment(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	s.mu.RLock()
	payment, ok := s.payments[id]
	s.mu.RUnlock()

	if !ok {
		httphelper.WriteError(w, http.StatusNotFound, "payment not found")
		return
	}

	httphelper.WriteJSON(w, http.StatusOK, payment)
}

func (s *Simulator) handleConfirmPayment(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	s.mu.Lock()
	payment, ok := s.payments[id]
	if !ok {
		s.mu.Unlock()
		httphelper.WriteError(w, http.StatusNotFound, "payment not found")
		return
	}

	payment.Status = "SUCCEEDED"
	payment.UpdatedAt = time.Now()
	pCopy := *payment
	s.mu.Unlock()

	go s.dispatchWebhook(&pCopy)

	httphelper.WriteJSON(w, http.StatusOK, pCopy)
}

func (s *Simulator) handleRejectPayment(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	s.mu.Lock()
	payment, ok := s.payments[id]
	if !ok {
		s.mu.Unlock()
		httphelper.WriteError(w, http.StatusNotFound, "payment not found")
		return
	}

	payment.Status = "FAILED"
	payment.UpdatedAt = time.Now()
	pCopy := *payment
	s.mu.Unlock()

	go s.dispatchWebhook(&pCopy)

	httphelper.WriteJSON(w, http.StatusOK, pCopy)
}

func (s *Simulator) dispatchWebhook(payment *PaymentRecord) {
	if payment.WebhookURL == "" {
		return
	}

	payload := WebhookPayload{
		EventID:     fmt.Sprintf("pay_evt_%s", uuid.New().String()),
		PaymentID:   payment.ID,
		ShipmentID:  payment.ShipmentID,
		Status:      payment.Status,
		AmountCents: payment.AmountCents,
		Currency:    payment.Currency,
		Timestamp:   time.Now(),
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return
	}

	for attempt := 0; attempt < 3; attempt++ {
		req, err := http.NewRequestWithContext(context.Background(), http.MethodPost, payment.WebhookURL, bytes.NewReader(body))
		if err != nil {
			return
		}
		req.Header.Set("Content-Type", "application/json")
		resp, err := s.httpClient.Do(req)
		if err == nil {
			resp.Body.Close()
			if resp.StatusCode >= 200 && resp.StatusCode < 300 {
				return
			}
		}
		time.Sleep(500 * time.Millisecond * time.Duration(attempt+1))
	}
}

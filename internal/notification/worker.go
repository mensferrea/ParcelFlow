package notification

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"time"

	"github.com/google/uuid"
	natsgo "github.com/nats-io/nats.go"

	"ParcelFlow/internal/domain"
	"ParcelFlow/internal/shipment/repository"
	"ParcelFlow/pkg/nats"
)

type Worker struct {
	repo       *repository.Repository
	natsClient *nats.Client
	httpClient *http.Client
	logger     *slog.Logger
	sub        *natsgo.Subscription
}

func NewWorker(repo *repository.Repository, nc *nats.Client, logger *slog.Logger) *Worker {
	return &Worker{
		repo:       repo,
		natsClient: nc,
		httpClient: &http.Client{Timeout: 5 * time.Second},
		logger:     logger,
	}
}

func (w *Worker) Start(ctx context.Context) error {
	sub, err := w.natsClient.JS.QueueSubscribe(
		nats.SubjectAll,
		"notification-workers",
		func(msg *natsgo.Msg) {
			w.handleMessage(ctx, msg)
		},
		natsgo.Durable("notification-worker-durable"),
		natsgo.ManualAck(),
		natsgo.AckWait(10*time.Second),
	)
	if err != nil {
		return fmt.Errorf("queue subscribe: %w", err)
	}

	w.sub = sub
	w.logger.Info("notification worker subscribed to JetStream stream")
	return nil
}

func (w *Worker) Stop() {
	if w.sub != nil {
		_ = w.sub.Unsubscribe()
	}
}

func (w *Worker) handleMessage(ctx context.Context, msg *natsgo.Msg) {
	var payload map[string]any
	if err := json.Unmarshal(msg.Data, &payload); err != nil {
		w.logger.Error("malformed message payload", "error", err)
		_ = msg.Term()
		return
	}

	eventID, _ := payload["event_id"].(string)
	eventType := msg.Subject

	subs, err := w.repo.GetActiveSubscriptionsForEvent(ctx, eventType)
	if err != nil {
		w.logger.Error("failed to get active webhook subscriptions", "error", err)
		_ = msg.NakWithDelay(2 * time.Second)
		return
	}

	for _, sub := range subs {
		w.deliverWebhook(ctx, sub, eventID, msg.Data)
	}

	_ = msg.Ack()
}

func (w *Worker) deliverWebhook(ctx context.Context, sub domain.WebhookSubscription, eventID string, body []byte) {
	maxAttempts := 3
	for attempt := 1; attempt <= maxAttempts; attempt++ {
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, sub.TargetURL, bytes.NewReader(body))
		if err != nil {
			return
		}

		mac := hmac.New(sha256.New, []byte(sub.Secret))
		mac.Write(body)
		signature := hex.EncodeToString(mac.Sum(nil))

		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-Signature-SHA256", signature)
		req.Header.Set("X-Event-ID", eventID)

		resp, err := w.httpClient.Do(req)
		var statusCode *int
		var respBody *string
		var errMsg *string
		success := false

		if err != nil {
			errStr := err.Error()
			errMsg = &errStr
		} else {
			sc := resp.StatusCode
			statusCode = &sc
			respBytes, _ := io.ReadAll(resp.Body)
			resp.Body.Close()
			rb := string(respBytes)
			respBody = &rb
			if sc >= 200 && sc < 300 {
				success = true
			} else {
				errStr := fmt.Sprintf("non-2xx response: %d", sc)
				errMsg = &errStr
			}
		}

		delivery := &domain.WebhookDelivery{
			ID:             uuid.New(),
			SubscriptionID: sub.ID,
			EventID:        eventID,
			StatusCode:     statusCode,
			Attempt:        attempt,
			RequestBody:    string(body),
			ResponseBody:   respBody,
			Success:        success,
			ErrorMessage:   errMsg,
			CreatedAt:      time.Now(),
		}

		_ = w.repo.LogWebhookDelivery(ctx, delivery)

		if success {
			return
		}

		select {
		case <-ctx.Done():
			return
		case <-time.After(time.Duration(attempt) * time.Second):
		}
	}
}

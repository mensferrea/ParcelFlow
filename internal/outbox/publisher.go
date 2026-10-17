package outbox

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"ParcelFlow/internal/shipment/repository"
	"ParcelFlow/pkg/nats"
)

type Publisher struct {
	repo       *repository.Repository
	natsClient *nats.Client
	logger     *slog.Logger
	stopChan   chan struct{}
}

func NewPublisher(repo *repository.Repository, natsClient *nats.Client, logger *slog.Logger) *Publisher {
	return &Publisher{
		repo:       repo,
		natsClient: natsClient,
		logger:     logger,
		stopChan:   make(chan struct{}),
	}
}

func (p *Publisher) Start(ctx context.Context, interval time.Duration) {
	ticker := time.NewTicker(interval)
	go func() {
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-p.stopChan:
				return
			case <-ticker.C:
				p.processEvents(ctx)
			}
		}
	}()
}

func (p *Publisher) Stop() {
	close(p.stopChan)
}

func (p *Publisher) processEvents(ctx context.Context) {
	events, err := p.repo.FetchPendingOutboxEvents(ctx, 50)
	if err != nil {
		p.logger.Error("failed to fetch pending outbox events", "error", err)
		return
	}

	for _, event := range events {
		subject := fmt.Sprintf("%s%s", nats.SubjectPrefix, event.EventType)
		_, err := p.natsClient.JS.Publish(subject, event.Payload)
		if err != nil {
			p.logger.Error("failed to publish outbox event to JetStream", "event_id", event.ID, "error", err)
			_ = p.repo.MarkOutboxEventFailed(ctx, event.ID, err.Error())
			continue
		}

		if err := p.repo.MarkOutboxEventPublished(ctx, event.ID); err != nil {
			p.logger.Error("failed to mark outbox event published", "event_id", event.ID, "error", err)
		}
	}
}

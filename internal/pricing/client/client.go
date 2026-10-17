package client

import (
	"context"
	"fmt"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	"ParcelFlow/internal/domain"
	pricingv1 "ParcelFlow/proto/pricing/v1"
)

type PricingClient struct {
	client pricingv1.PricingServiceClient
	conn   *grpc.ClientConn
}

func NewPricingClient(target string) (*PricingClient, error) {
	conn, err := grpc.NewClient(
		target,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		return nil, fmt.Errorf("connect to pricing service: %w", err)
	}

	return &PricingClient{
		client: pricingv1.NewPricingServiceClient(conn),
		conn:   conn,
	}, nil
}

func (c *PricingClient) Close() error {
	if c.conn != nil {
		return c.conn.Close()
	}
	return nil
}

func (c *PricingClient) CalculateRate(ctx context.Context, input domain.CreateShipmentInput) (*pricingv1.CalculateRateResponse, error) {
	var lastErr error
	for attempt := 0; attempt < 3; attempt++ {
		callCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
		res, err := c.client.CalculateRate(callCtx, &pricingv1.CalculateRateRequest{
			OriginCity:      input.OriginCity,
			DestinationCity: input.DestinationCity,
			WeightKg:        input.WeightKg,
			LengthCm:        input.LengthCm,
			WidthCm:         input.WidthCm,
			HeightCm:        input.HeightCm,
			DeliveryType:    string(input.DeliveryType),
		})
		cancel()

		if err == nil {
			return res, nil
		}

		lastErr = err
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(100 * time.Millisecond * time.Duration(attempt+1)):
		}
	}

	return nil, fmt.Errorf("pricing service unavailable after retries: %w", lastErr)
}

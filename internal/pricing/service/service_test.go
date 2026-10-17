package service_test

import (
	"context"
	"testing"

	"ParcelFlow/internal/domain"
	"ParcelFlow/internal/pricing/service"
)

func TestPricingCalculation(t *testing.T) {
	svc := service.NewPricingService()
	ctx := context.Background()

	res, err := svc.CalculateRate(ctx, "Ufa", "Kazan", 2.5, 30, 20, 15, domain.DeliveryTypeStandard)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if res.PriceCents <= 0 {
		t.Errorf("expected positive price, got %d", res.PriceCents)
	}
	if res.DistanceKm != 530 {
		t.Errorf("expected distance 530, got %f", res.DistanceKm)
	}
	if res.Currency != "RUB" {
		t.Errorf("expected RUB, got %s", res.Currency)
	}
}

func TestPricingExpressRateHigher(t *testing.T) {
	svc := service.NewPricingService()
	ctx := context.Background()

	stdRes, _ := svc.CalculateRate(ctx, "Moscow", "SPb", 5, 20, 20, 20, domain.DeliveryTypeStandard)
	expRes, _ := svc.CalculateRate(ctx, "Moscow", "SPb", 5, 20, 20, 20, domain.DeliveryTypeExpress)

	if expRes.PriceCents <= stdRes.PriceCents {
		t.Errorf("express rate (%d) should be greater than standard (%d)", expRes.PriceCents, stdRes.PriceCents)
	}
}

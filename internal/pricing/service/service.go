package service

import (
	"context"
	"math"
	"strings"

	"ParcelFlow/internal/domain"
	pricingv1 "ParcelFlow/proto/pricing/v1"
)

type PricingService struct {
	cityDistances map[string]float64
}

func NewPricingService() *PricingService {
	return &PricingService{
		cityDistances: map[string]float64{
			"ufa-kazan":        530,
			"kazan-ufa":        530,
			"ufa-moscow":       1350,
			"moscow-ufa":       1350,
			"kazan-moscow":     820,
			"moscow-kazan":     820,
			"moscow-spb":       710,
			"spb-moscow":       710,
			"ufa-ekaterinburg": 520,
			"ekaterinburg-ufa": 520,
			"kazan-samara":     360,
			"samara-kazan":     360,
			"ufa-samara":       460,
			"samara-ufa":       460,
		},
	}
}

func (s *PricingService) CalculateRate(ctx context.Context, origin, destination string, weightKg, lengthCm, widthCm, heightCm float64, deliveryType domain.DeliveryType) (*pricingv1.CalculateRateResponse, error) {
	distance := s.getDistance(origin, destination)

	volumetricWeight := (lengthCm * widthCm * heightCm) / 5000.0
	chargeableWeight := math.Max(weightKg, volumetricWeight)
	if chargeableWeight < 0.5 {
		chargeableWeight = 0.5
	}

	var baseRateCents float64 = 25000
	var perKgRateCents float64 = 5000
	var perKmRateCents float64 = 15

	var multiplier float64 = 1.0
	var speedKmPerDay float64 = 500.0

	switch deliveryType {
	case domain.DeliveryTypeExpress:
		multiplier = 1.8
		speedKmPerDay = 1000.0
	case domain.DeliveryTypeFreight:
		multiplier = 1.2
		perKgRateCents = 3500
		speedKmPerDay = 400.0
	default:
		multiplier = 1.0
	}

	totalCents := (baseRateCents + (chargeableWeight * perKgRateCents) + (distance * perKmRateCents)) * multiplier
	estimatedDays := int32(math.Ceil(distance / speedKmPerDay))
	if estimatedDays < 1 {
		estimatedDays = 1
	}

	return &pricingv1.CalculateRateResponse{
		PriceCents:    int64(math.Round(totalCents)),
		Currency:      "RUB",
		DistanceKm:    distance,
		EstimatedDays: estimatedDays,
		DeliveryType:  string(deliveryType),
	}, nil
}

func (s *PricingService) getDistance(origin, destination string) float64 {
	key := strings.ToLower(strings.TrimSpace(origin)) + "-" + strings.ToLower(strings.TrimSpace(destination))
	if dist, ok := s.cityDistances[key]; ok {
		return dist
	}
	if strings.EqualFold(origin, destination) {
		return 30.0
	}
	return 650.0
}

func (s *PricingService) GetTariffs(ctx context.Context) []*pricingv1.TariffInfo {
	return []*pricingv1.TariffInfo{
		{
			DeliveryType:  string(domain.DeliveryTypeStandard),
			BaseRateCents: 25000,
			PerKgCents:    5000,
			PerKmCents:    15,
			MinDays:       2,
			MaxDays:       5,
		},
		{
			DeliveryType:  string(domain.DeliveryTypeExpress),
			BaseRateCents: 45000,
			PerKgCents:    9000,
			PerKmCents:    27,
			MinDays:       1,
			MaxDays:       2,
		},
		{
			DeliveryType:  string(domain.DeliveryTypeFreight),
			BaseRateCents: 30000,
			PerKgCents:    3500,
			PerKmCents:    18,
			MinDays:       3,
			MaxDays:       7,
		},
	}
}

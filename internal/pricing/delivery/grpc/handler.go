package grpc

import (
	"context"

	"ParcelFlow/internal/domain"
	"ParcelFlow/internal/pricing/service"
	pricingv1 "ParcelFlow/proto/pricing/v1"
)

type Server struct {
	pricingv1.UnimplementedPricingServiceServer
	service *service.PricingService
}

func NewServer(svc *service.PricingService) *Server {
	return &Server{service: svc}
}

func (s *Server) CalculateRate(ctx context.Context, req *pricingv1.CalculateRateRequest) (*pricingv1.CalculateRateResponse, error) {
	return s.service.CalculateRate(
		ctx,
		req.GetOriginCity(),
		req.GetDestinationCity(),
		req.GetWeightKg(),
		req.GetLengthCm(),
		req.GetWidthCm(),
		req.GetHeightCm(),
		domain.DeliveryType(req.GetDeliveryType()),
	)
}

func (s *Server) GetTariffs(ctx context.Context, req *pricingv1.GetTariffsRequest) (*pricingv1.GetTariffsResponse, error) {
	tariffs := s.service.GetTariffs(ctx)
	return &pricingv1.GetTariffsResponse{Tariffs: tariffs}, nil
}

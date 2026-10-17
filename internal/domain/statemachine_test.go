package domain_test

import (
	"testing"

	"ParcelFlow/internal/domain"
)

func TestValidateTransition(t *testing.T) {
	tests := []struct {
		name    string
		from    domain.ShipmentStatus
		to      domain.ShipmentStatus
		wantErr bool
	}{
		{"Created to WaitingForPayment", domain.StatusCreated, domain.StatusWaitingForPayment, false},
		{"Created to Cancelled", domain.StatusCreated, domain.StatusCancelled, false},
		{"Created to Delivered (invalid)", domain.StatusCreated, domain.StatusDelivered, true},
		{"WaitingForPayment to Paid", domain.StatusWaitingForPayment, domain.StatusPaid, false},
		{"WaitingForPayment to PaymentFailed", domain.StatusWaitingForPayment, domain.StatusPaymentFailed, false},
		{"Paid to Accepted", domain.StatusPaid, domain.StatusAccepted, false},
		{"Accepted to InTransit", domain.StatusAccepted, domain.StatusInTransit, false},
		{"InTransit to OutForDelivery", domain.StatusInTransit, domain.StatusOutForDelivery, false},
		{"OutForDelivery to Delivered", domain.StatusOutForDelivery, domain.StatusDelivered, false},
		{"Delivered to InTransit (forbidden transition)", domain.StatusDelivered, domain.StatusInTransit, true},
		{"Cancelled to Created (forbidden transition)", domain.StatusCancelled, domain.StatusCreated, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := domain.ValidateTransition(tt.from, tt.to)
			if (err != nil) != tt.wantErr {
				t.Fatalf("ValidateTransition(%s, %s) error = %v, wantErr = %v", tt.from, tt.to, err, tt.wantErr)
			}
		})
	}
}

func TestCanCancel(t *testing.T) {
	if !domain.CanCancel(domain.StatusCreated) {
		t.Error("expected Created to be cancellable")
	}
	if !domain.CanCancel(domain.StatusWaitingForPayment) {
		t.Error("expected WaitingForPayment to be cancellable")
	}
	if !domain.CanCancel(domain.StatusPaymentFailed) {
		t.Error("expected PaymentFailed to be cancellable")
	}
	if domain.CanCancel(domain.StatusPaid) {
		t.Error("expected Paid NOT to be cancellable")
	}
	if domain.CanCancel(domain.StatusDelivered) {
		t.Error("expected Delivered NOT to be cancellable")
	}
}

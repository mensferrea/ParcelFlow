package domain

var validTransitions = map[ShipmentStatus][]ShipmentStatus{
	StatusCreated: {
		StatusWaitingForPayment,
		StatusCancelled,
	},
	StatusWaitingForPayment: {
		StatusPaid,
		StatusPaymentFailed,
		StatusCancelled,
	},
	StatusPaymentFailed: {
		StatusWaitingForPayment,
		StatusCancelled,
	},
	StatusPaid: {
		StatusAccepted,
	},
	StatusAccepted: {
		StatusInTransit,
	},
	StatusInTransit: {
		StatusOutForDelivery,
	},
	StatusOutForDelivery: {
		StatusDelivered,
	},
	StatusDelivered: {},
	StatusCancelled: {},
}

func ValidateTransition(current ShipmentStatus, next ShipmentStatus) error {
	allowed, exists := validTransitions[current]
	if !exists {
		return ErrInvalidStateTransition
	}

	for _, s := range allowed {
		if s == next {
			return nil
		}
	}

	return ErrInvalidStateTransition
}

func CanCancel(status ShipmentStatus) bool {
	return status == StatusCreated || status == StatusWaitingForPayment || status == StatusPaymentFailed
}

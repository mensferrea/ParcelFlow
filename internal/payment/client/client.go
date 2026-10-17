package client

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"ParcelFlow/internal/payment/simulator"
)

type PaymentClient struct {
	baseURL    string
	httpClient *http.Client
}

func NewPaymentClient(baseURL string) *PaymentClient {
	return &PaymentClient{
		baseURL:    baseURL,
		httpClient: &http.Client{Timeout: 5 * time.Second},
	}
}

func (c *PaymentClient) CreatePayment(ctx context.Context, req simulator.CreatePaymentRequest) (*simulator.PaymentRecord, error) {
	body, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("marshal payment request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/payments", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("create http request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("execute payment request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		return nil, fmt.Errorf("payment provider returned status: %d", resp.StatusCode)
	}

	var payment simulator.PaymentRecord
	if err := json.NewDecoder(resp.Body).Decode(&payment); err != nil {
		return nil, fmt.Errorf("decode payment response: %w", err)
	}

	return &payment, nil
}

func (c *PaymentClient) GetPayment(ctx context.Context, id string) (*simulator.PaymentRecord, error) {
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+"/payments/"+id, nil)
	if err != nil {
		return nil, fmt.Errorf("create http request: %w", err)
	}

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("execute get payment request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		return nil, fmt.Errorf("payment not found")
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("payment provider returned status: %d", resp.StatusCode)
	}

	var payment simulator.PaymentRecord
	if err := json.NewDecoder(resp.Body).Decode(&payment); err != nil {
		return nil, fmt.Errorf("decode payment response: %w", err)
	}

	return &payment, nil
}

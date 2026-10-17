package nats

import (
	"fmt"
	"time"

	"github.com/nats-io/nats.go"
)

const (
	StreamShipments = "SHIPMENTS"
	SubjectPrefix   = "shipments."
	SubjectAll      = "shipments.*"
)

type Client struct {
	Conn *nats.Conn
	JS   nats.JetStreamContext
}

func New(url string) (*Client, error) {
	nc, err := nats.Connect(url, nats.Timeout(5*time.Second), nats.MaxReconnects(10), nats.ReconnectWait(2*time.Second))
	if err != nil {
		return nil, fmt.Errorf("connect to nats: %w", err)
	}

	js, err := nc.JetStream()
	if err != nil {
		nc.Close()
		return nil, fmt.Errorf("jetstream context: %w", err)
	}

	_, err = js.StreamInfo(StreamShipments)
	if err != nil {
		_, err = js.AddStream(&nats.StreamConfig{
			Name:      StreamShipments,
			Subjects:  []string{SubjectAll},
			Storage:   nats.FileStorage,
			Retention: nats.LimitsPolicy,
		})
		if err != nil {
			nc.Close()
			return nil, fmt.Errorf("create stream %s: %w", StreamShipments, err)
		}
	}

	return &Client{
		Conn: nc,
		JS:   js,
	}, nil
}

func (c *Client) Close() {
	if c.Conn != nil {
		c.Conn.Close()
	}
}

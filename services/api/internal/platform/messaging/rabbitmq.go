// Package messaging provides a RabbitMQ AMQP publisher.
package messaging

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"

	amqp "github.com/rabbitmq/amqp091-go"
)

// Publisher wraps an AMQP channel and publishes JSON-encoded domain events to a
// named exchange.
type Publisher struct {
	conn     *amqp.Connection
	ch       *amqp.Channel
	exchange string
	log      *slog.Logger
}

// NewPublisher dials the broker, opens a channel, and declares the exchange.
func NewPublisher(url, exchange string, log *slog.Logger) (*Publisher, error) {
	conn, err := amqp.Dial(url)
	if err != nil {
		return nil, fmt.Errorf("messaging: dial: %w", err)
	}

	ch, err := conn.Channel()
	if err != nil {
		_ = conn.Close()
		return nil, fmt.Errorf("messaging: open channel: %w", err)
	}

	if err := ch.ExchangeDeclare(exchange, "topic", true, false, false, false, nil); err != nil {
		_ = conn.Close()
		return nil, fmt.Errorf("messaging: declare exchange: %w", err)
	}

	log.Info("rabbitmq connected", "exchange", exchange)
	return &Publisher{conn: conn, ch: ch, exchange: exchange, log: log}, nil
}

// Publish serialises payload as JSON and publishes it with the given routing key.
func (p *Publisher) Publish(ctx context.Context, routingKey string, payload any) error {
	if p == nil || p.ch == nil {
		return errors.New("messaging: publisher not initialized")
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("messaging: marshal: %w", err)
	}

	err = p.ch.PublishWithContext(ctx, p.exchange, routingKey, false, false,
		amqp.Publishing{
			ContentType: "application/json",
			Body:        body,
		},
	)
	if err != nil {
		return fmt.Errorf("messaging: publish %q: %w", routingKey, err)
	}

	return nil
}

// Close releases the channel and connection.
func (p *Publisher) Close() {
	if p == nil {
		return
	}
	if p.ch != nil {
		_ = p.ch.Close()
	}
	if p.conn != nil {
		_ = p.conn.Close()
	}
}

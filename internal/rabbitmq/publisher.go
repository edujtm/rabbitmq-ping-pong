package rabbitmq

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	amqp "github.com/rabbitmq/amqp091-go"
	"github.com/rs/zerolog/log"
)

type Publisher struct {
	client       *Client
	exchangeName string
	routingKey   string
}

func (pub *Publisher) PublishUnsafe(ctx context.Context, payload any) error {
	if !pub.client.isReady {
		return ErrNotConnected
	}

	data, err := json.Marshal(payload)
	if err != nil {
		return ErrBadPayload
	}

	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	return pub.client.channel.PublishWithContext(
		ctx,
		pub.exchangeName,
		pub.routingKey,
		false,
		false,
		amqp.Publishing{
			ContentType: "application/json",
			Body:        data,
		},
	)
}

func (pub *Publisher) Publish(ctx context.Context, payload any) error {
	if !pub.client.isReady {
		return errors.New("failed to publish message. not connected to rabbitmq")
	}

	for {
		err := pub.PublishUnsafe(ctx, payload)
		if err != nil {
			if errors.Is(err, ErrBadPayload) {
				return err
			}

			log.Warn().
				Str("routing_key", pub.routingKey).
				Str("exchange_name", pub.exchangeName).
				Msg("Failed to push: Retrying...")

			select {
			case <-pub.client.done:
				return ErrShutdown
			case <-time.After(resendDelay):
			}
			continue
		}

		// Wait for the confirmation message from rabbitmq
		confirm := <-pub.client.notifyConfirm
		if confirm.Ack {
			log.Debug().
				Uint64("delivery_tag", confirm.DeliveryTag).
				Msg("Message publish confirmed.")
			return nil
		}
	}
}

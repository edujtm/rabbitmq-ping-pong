package rabbitmq

import (
	"time"

	"github.com/edujtm/rabbit-ping-pong/internal/messageq"
	amqp "github.com/rabbitmq/amqp091-go"
	"github.com/rs/zerolog/log"
)

type Consumer struct {
	client       *Client
	done         <-chan interface{}
	queueName    string
	consumerName string
}

func (cons *Consumer) Consume() (<-chan messageq.Payload[[]byte], error) {

	consumerStream := make(chan messageq.Payload[[]byte])
	go func() {
		defer close(consumerStream)

		cons.waitForConnection()
		cons.configureQos()

		for {
			deliveries, err := cons.consume()
			if err != nil {
				time.Sleep(3 * time.Second)
				continue
			}

			log.Info().
				Msgf(
					"Rabbitmq consumer to queue '%s' with name '%s' created successfully",
					cons.queueName,
					cons.consumerName,
				)
			chanClosed := cons.client.chanCloseNotifier()

		outer:
			for {
				select {
				case <-cons.done:
					return
				case <-chanClosed:
					log.Warn().Msg("Channel was closed. Retrying...")
					break outer
				case message := <-deliveries:
					consumerStream <- NewPayload(message, message.Body)
				}
			}
		}
	}()

	return consumerStream, nil
}

// Waits for the background goroutine that handles the rabbitmq connection
// to be completely setup.
func (cons *Consumer) waitForConnection() {
	for !cons.client.isReady {
		time.Sleep(1 * time.Second)
	}
}

func (cons *Consumer) configureQos() {
	for {
		err := cons.client.channel.Qos(1, 0, false)
		if err != nil {
			log.Debug().Msg("Couldn't configure Qos. Retrying in 3s...")
			time.Sleep(3 * time.Second)
			continue
		} else {
			break
		}
	}
}

func (cons *Consumer) consume() (<-chan amqp.Delivery, error) {
	return cons.client.channel.Consume(
		cons.queueName,
		"",
		false,
		false,
		false,
		false,
		nil,
	)
}

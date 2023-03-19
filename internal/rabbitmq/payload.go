package rabbitmq

import amqp "github.com/rabbitmq/amqp091-go"

type Payload struct {
	data     []byte
	err      error
	delivery amqp.Delivery
}

func NewPayload(delivery amqp.Delivery, data []byte) Payload {
	return Payload{data: data, delivery: delivery}
}

func NewErrorPayload(delivery amqp.Delivery, err error) Payload {
	return Payload{err: err, delivery: delivery}
}

func (p Payload) Data() []byte {
	return p.data
}

func (p Payload) Err() error {
	return p.err
}

func (p Payload) Ack() error {
	return p.delivery.Ack(false)
}

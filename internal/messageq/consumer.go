package messageq

type Payload[T any] interface {
	Data() T
	Err() error
	Ack() error
}

type Consumer[T any] interface {
	Consume() (<-chan Payload[T], error)
}

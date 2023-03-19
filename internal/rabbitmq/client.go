package rabbitmq

import (
	"errors"
	"time"

	amqp "github.com/rabbitmq/amqp091-go"
	"github.com/rs/zerolog/log"
)

const (
	resendDelay    = 5 * time.Second
	reconnectDelay = 3 * time.Second
	reInitDelay    = 5 * time.Second
)

var (
	ErrNotConnected  = errors.New("not connected to a server")
	ErrShutdown      = errors.New("client is shutting down")
	ErrAlreadyClosed = errors.New("couldn't close rabbitmq client. not connected to server")
	ErrBadPayload    = errors.New("error publishing to rabbitmq. couldn't serialize payload")
)

type ClientConfig struct {
	ExchangeName string
	QueueName    string
	Addr         string
}

// Implements a RabbitMQ client with reconnection support
// based on the examples from the amqp library maintainers.
type Client struct {
	setupDeclarations func(*amqp.Channel) error
	conn              *amqp.Connection
	channel           *amqp.Channel
	notifyConnClose   chan *amqp.Error
	notifyChanClose   chan *amqp.Error
	notifyConfirm     chan amqp.Confirmation
	done              chan bool
	consumersDone     []chan interface{}
	isReady           bool
}

func Connect(addr string) *Client {
	client := Client{
		done: make(chan bool),
	}

	// Reconnection logic is handled by a background goroutine
	go client.handleReconnect(addr)
	return &client
}

// Configures the queue, exchanges and bindings declarations.
//
// This function will be called everytime the client reconnects
// to the rabbitmq broker (in case there was a failure that
// interrupted the connection).
//
// The client handles reconnection automatically, so this setup
// needs to be done only once at app startup.
func (client *Client) ConfigureDeclarations(setup func(*amqp.Channel) error) {
	client.setupDeclarations = setup
}

func (client *Client) handleReconnect(addr string) {
	for {
		client.isReady = false
		log.Info().Msgf("Attempting to connect to rabbitmq at %s", addr)
		conn, err := client.connect(addr)
		if err != nil {
			log.Warn().Msgf("Coudn't connect to rabbitmq. Retrying after %ds delay", int64(reconnectDelay.Seconds()))
			select {
			case <-client.done: // client is shutting down, stop trying to reconnect
				return
			case <-time.After(reconnectDelay): // Attempt reconnect after delay
			}
			continue
		}

		// this call blocks until there's a connection closed notification
		if done := client.reinitializeChannel(conn); done {
			break
		}
	}
}

func (client *Client) reinitializeChannel(conn *amqp.Connection) bool {
	for {
		client.isReady = false

		err := client.init(conn)
		if err != nil {
			select {
			case <-client.done:
				return true
			case <-time.After(reInitDelay):
			}
			continue
		}

		// Block the goroutine until either the client itself is closed
		// or there's a connection closed notification from rabbitmq
		select {
		case <-client.done:
			return true
		case <-client.notifyConnClose:
			log.Debug().Msg("Rabbitmq connection was closed by the server")
			return false
		case <-client.notifyChanClose: // If the channel is closed, attempt reconnection
		}
	}
}

func (client *Client) init(conn *amqp.Connection) error {
	ch, err := conn.Channel()
	if err != nil {
		return err
	}

	// Confirmations are necessary for reliable publishing
	err = ch.Confirm(false)
	if err != nil {
		return err
	}

	// Allows the declarations to be defined by
	// the user.
	err = client.setupDeclarations(ch)
	if err != nil {
		return err
	}

	client.changeChannel(ch)
	client.isReady = true
	log.Info().Msg("RabbitMQ client setup completed")

	return nil
}

func (client *Client) changeConnection(connection *amqp.Connection) {
	client.conn = connection
	client.notifyConnClose = make(chan *amqp.Error, 1)
	client.conn.NotifyClose(client.notifyConnClose)
}

func (client *Client) changeChannel(channel *amqp.Channel) {
	client.channel = channel

	// There's no problem with changing the notify channel
	// references here because those channels get closed
	// when the amqp.Channel is closed. Since we only
	// get here when either the client is not initialized
	// or when attempting a reconnection, then any consumer
	// listening for events on these channels will receive
	// an event on close and will not deadlock.
	client.notifyChanClose = make(chan *amqp.Error, 1)
	client.notifyConfirm = make(chan amqp.Confirmation, 1)
	client.channel.NotifyClose(client.notifyChanClose)
	client.channel.NotifyPublish(client.notifyConfirm)
}

func (client *Client) chanCloseNotifier() <-chan *amqp.Error {
	notifyChanClose := make(chan *amqp.Error, 1)
	client.channel.NotifyClose(notifyChanClose)
	return notifyChanClose
}

func (client *Client) connect(addr string) (*amqp.Connection, error) {
	conn, err := amqp.Dial(addr)
	if err != nil {
		return nil, err
	}

	client.changeConnection(conn)
	return conn, nil
}

func (client *Client) NewPublisher(exchangeName string, routingKey string) *Publisher {
	return &Publisher{client, exchangeName, routingKey}
}

func (client *Client) NewConsumer(queueName string, consumerName string) *Consumer {
	consumerDone := make(chan interface{})
	client.consumersDone = append(client.consumersDone, consumerDone)

	return &Consumer{
		client:       client,
		done:         consumerDone,
		queueName:    queueName,
		consumerName: consumerName,
	}
}

func (client *Client) Close() error {
	if !client.isReady {
		return ErrAlreadyClosed
	}

	// Closes the done channel first to avoid the reconnection
	// logic from trying to start new connections on the
	// background goroutine
	close(client.done)

	// Allows the consumer goroutines to get out of the
	// infinite loop that handles reconnection.
	for _, done := range client.consumersDone {
		close(done)
	}

	err := client.channel.Close()
	if err != nil {
		return err
	}

	err = client.conn.Close()
	if err != nil {
		return err
	}

	client.isReady = false
	return nil
}

package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"net/http"
	"os"
	"sync"
	"time"

	"github.com/edujtm/rabbit-ping-pong/internal/httputil"
	"github.com/edujtm/rabbit-ping-pong/internal/messageq"
	"github.com/edujtm/rabbit-ping-pong/internal/rabbitmq"
	amqp "github.com/rabbitmq/amqp091-go"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
)

const (
	PING_EXCHANGE    = "ping-exchange"
	PING_QUEUE       = "ping-queue"
	PING_ROUTING_KEY = "ping-me"
	PONG_EXCHANGE    = "pong-exchange"
	PONG_QUEUE       = "pong-queue"
	PONG_ROUTING_KEY = "pong-me"
)

type Config struct {
	RabbitAddr string
	Port       string
}

func main() {

	var cfg Config

	flag.StringVar(&cfg.RabbitAddr, "rabbit-url", os.Getenv("RABBITMQ_SERVER_URL"), "The RabbitMQ server address")
	flag.StringVar(&cfg.Port, "port", os.Getenv("LISTEN_PORT"), "The port where the service will listen for incoming connections")

	flag.Parse()

	logWriter := zerolog.ConsoleWriter{Out: os.Stdout}
	log.Logger = log.Output(logWriter)

	client := rabbitmq.Connect(cfg.RabbitAddr)
	defer client.Close()

	client.ConfigureDeclarations(DeclareQueueAndExchanges)

	publishers := Publishers{Pong: client.NewPublisher(PONG_EXCHANGE, PONG_ROUTING_KEY)}
	app := App{Publishers: publishers}

	err := app.startPingWorker(client.NewConsumer(PING_QUEUE, "ponger"))
	if err != nil {
		log.Fatal()
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/ping-count", app.pingCountHandler)

	server := &http.Server{
		Addr:         fmt.Sprintf(":%s", cfg.Port),
		Handler:      mux,
		IdleTimeout:  1 * time.Minute,
		ReadTimeout:  10 * time.Minute,
		WriteTimeout: 10 * time.Minute,
	}

	shutdownFinished := httputil.SetupGracefulShutdown(server, 1*time.Minute)

	if err := server.ListenAndServe(); err != http.ErrServerClosed {
		log.Fatal().Err(err).Msg("Error ocurred while trying to listen for connections.")
	}

	<-shutdownFinished
}

type Publishers struct {
	Pong messageq.Publisher
}

type State struct {
	lock      sync.Mutex
	PingCount uint64
}

type App struct {
	Publishers Publishers
	State      State
}

func (app *App) pingCountHandler(w http.ResponseWriter, r *http.Request) {
	app.State.lock.Lock()
	defer app.State.lock.Unlock()

	pingCount := app.State.PingCount
	w.WriteHeader(http.StatusOK)
	w.Write([]byte(fmt.Sprintf("Received %d pings.\n", pingCount)))
}

func (app *App) startPingWorker(consumer messageq.Consumer[[]byte]) error {
	pings, err := consumer.Consume()
	if err != nil {
		return err
	}

	go func() {

		for ping := range pings {
			var payload map[string]string
			err := json.Unmarshal(ping.Data(), &payload)
			if err != nil {
				log.Warn().Msg("Some weird pinging occurred!")
			} else {
				data := payload["data"]

				if data == "ping" {
					app.State.lock.Lock()
					app.State.PingCount += 1
					app.State.lock.Unlock()

					log.Info().Msg("You've pinged me. Better watch out for my pong.")
					app.Publishers.Pong.Publish(context.Background(), map[string]string{"data": "pong"})
				} else {
					log.Info().Msg("Stop pinging weird things!")
				}
			}

			ping.Ack()
		}
	}()

	return nil
}

func DeclareQueueAndExchanges(channel *amqp.Channel) error {

	err := channel.ExchangeDeclare(PONG_EXCHANGE, "direct", true, false, false, false, nil)
	if err != nil {
		return err
	}

	_, err = channel.QueueDeclare(PING_QUEUE, true, false, false, false, nil)
	if err != nil {
		return err
	}

	_, err = channel.QueueDeclare(PONG_QUEUE, true, false, false, false, nil)
	if err != nil {
		return err
	}

	err = channel.QueueBind(PONG_QUEUE, PONG_ROUTING_KEY, PONG_EXCHANGE, false, nil)
	if err != nil {
		return err
	}

	return nil
}

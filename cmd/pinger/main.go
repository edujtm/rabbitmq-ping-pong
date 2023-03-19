package main

import (
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
	PONG_QUEUE       = "pong-queue"
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

	publishers := Publishers{Ping: client.NewPublisher(PING_EXCHANGE, PING_ROUTING_KEY)}
	app := App{Publishers: publishers}

	err := app.startPongWorker(client.NewConsumer(PONG_QUEUE, "ponger"))
	if err != nil {
		log.Fatal().Err(err).Msg("Error occurred setting up workers")
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/ping", app.pingHandler)
	mux.HandleFunc("/pong-count", app.pongCountHandler)

	server := &http.Server{
		Addr:         fmt.Sprintf(":%s", cfg.Port),
		Handler:      mux,
		IdleTimeout:  1 * time.Minute,
		ReadTimeout:  10 * time.Minute,
		WriteTimeout: 10 * time.Minute,
	}

	shutdownFinished := httputil.SetupGracefulShutdown(server, 1*time.Minute)

	err = server.ListenAndServe()
	if err != nil && err != http.ErrServerClosed {
		log.Fatal().Err(err).Msg("Some error occurred while trying to listen for connections.")
	}

	<-shutdownFinished
}

type Publishers struct {
	Ping messageq.Publisher
}

type State struct {
	lock      sync.Mutex
	PongCount uint64
}

type App struct {
	Publishers Publishers
	State      State
}

func (app *App) pingHandler(w http.ResponseWriter, r *http.Request) {
	log.Info().Msg("I will ping now!")

	err := app.Publishers.Ping.Publish(r.Context(), map[string]string{"data": "ping"})
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte("Coudln't publish!\n"))
	} else {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("Sucessfully pinged!\n"))
	}
}

func (app *App) pongCountHandler(w http.ResponseWriter, r *http.Request) {
	app.State.lock.Lock()
	defer app.State.lock.Unlock()

	count := app.State.PongCount
	w.WriteHeader(http.StatusOK)
	w.Write([]byte(fmt.Sprintf("Received %d pongs.\n", count)))
}

func (app *App) startPongWorker(consumer messageq.Consumer[[]byte]) error {
	pongs, err := consumer.Consume()
	if err != nil {
		return err
	}

	go func() {
		for pong := range pongs {

			var payload map[string]string
			err := json.Unmarshal(pong.Data(), &payload)

			if err != nil {
				log.Warn().Msg("Some weird ponging occurred!")
			} else {
				pong := payload["data"]

				if pong == "pong" {
					app.State.lock.Lock()
					app.State.PongCount += 1
					app.State.lock.Unlock()

					log.Info().Msg("I've been successfully ponged!")
				} else {
					log.Info().Msg("That's not a pong!")
				}
			}

			pong.Ack()
		}
	}()
	return nil
}

func DeclareQueueAndExchanges(channel *amqp.Channel) error {

	err := channel.ExchangeDeclare(PING_EXCHANGE, "direct", true, false, false, false, nil)
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

	err = channel.QueueBind(PING_QUEUE, PING_ROUTING_KEY, PING_EXCHANGE, false, nil)
	if err != nil {
		return err
	}

	return nil
}

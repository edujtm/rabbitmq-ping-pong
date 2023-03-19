package httputil

import (
	"context"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/rs/zerolog/log"
)

func SetupGracefulShutdown(server *http.Server, timeout time.Duration) <-chan interface{} {
	shutdownFinished := make(chan interface{})

	go func() {
		defer close(shutdownFinished)

		interrupt := make(chan os.Signal, 1)
		signal.Notify(interrupt, syscall.SIGINT, syscall.SIGTERM)
		<-interrupt

		ctx, cancel := context.WithTimeout(context.Background(), timeout)
		defer cancel()

		if err := server.Shutdown(ctx); err != nil {
			log.Error().Err(err).Msg("Error occurred during server shutdown")
		}
	}()

	return shutdownFinished
}

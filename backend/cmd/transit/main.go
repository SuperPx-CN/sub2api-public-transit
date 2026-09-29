package main

import (
	"context"
	"errors"
	"github.com/Wei-Shaw/sub2api/internal/transit"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"
)

func main() {
	c, err := transit.LoadConfig()
	if err != nil {
		log.Fatal(err)
	}
	s, err := transit.Open(c)
	if err != nil {
		log.Fatal("unable to initialize local service configuration")
	}
	defer s.DB.Close()
	server := &http.Server{Addr: c.Address, Handler: transit.NewHandler(s, c), ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 10 * time.Second, WriteTimeout: 25 * time.Second, IdleTimeout: 60 * time.Second, MaxHeaderBytes: 16384}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	go func() {
		<-ctx.Done()
		shutdown, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		server.Shutdown(shutdown)
	}()
	log.Printf("ai-transit.v1 listening on %s", c.Address)
	if err = server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		log.Fatal("HTTP server stopped unexpectedly")
	}
}

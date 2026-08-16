package main

import (
	"context"
	"errors"
	"log"
	"net/http"
	"os/signal"
	"syscall"
	"time"

	"arcticdispatch/internal/arrival"
	"arcticdispatch/internal/booking"
	"arcticdispatch/internal/cargo"
	"arcticdispatch/internal/clock"
	"arcticdispatch/internal/config"
	"arcticdispatch/internal/dispatch"
	"arcticdispatch/internal/httpapi"
	"arcticdispatch/internal/manifest"
	"arcticdispatch/internal/portapp"
	"arcticdispatch/internal/scheduler"
	"arcticdispatch/internal/store"
)

func main() {
	cfg, err := config.Load("config.json")
	if err != nil {
		log.Fatalf("config: %v", err)
	}
	st, err := store.New(cfg.StorePath)
	if err != nil {
		log.Fatalf("store: %v", err)
	}
	clk := clock.System{}
	cargoSvc := cargo.New(st, clk)
	bookingSvc := booking.New(st, clk, cfg.PaymentTimeout)
	portappSvc := portapp.New(st, clk)
	dispatchSvc := dispatch.New(st, clk, portappSvc)
	manifestSvc := manifest.New(st, clk)
	arrivalSvc := arrival.New(st, clk)
	sched := scheduler.New(bookingSvc, portappSvc, cfg.SchedulerInterval)

	// Startup reconciliation (failure recovery): release freezes that expired
	// while the service was down and re-schedule appointments for voyages left
	// delayed by a crash.
	sched.ReconcileNow()

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	sched.Start(ctx)

	srv := &http.Server{
		Addr:              ":" + cfg.Port,
		Handler:           http.TimeoutHandler(httpapi.New(st, cargoSvc, bookingSvc, dispatchSvc, portappSvc, manifestSvc, arrivalSvc), 30*time.Second, `{"error":"timeout"}`),
		ReadHeaderTimeout: 5 * time.Second,
	}
	log.Printf("arcticdispatch listening on :%s (store=%s)", cfg.Port, cfg.StorePath)
	go func() {
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Fatalf("server: %v", err)
		}
	}()
	<-ctx.Done()
	stop()
	log.Println("shutting down")
	shutCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_ = srv.Shutdown(shutCtx)
	sched.Stop()
}

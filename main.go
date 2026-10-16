package main

import (
	"context"
	"errors"
	"flag"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/kiran94/openitinerary-batch-processing/internal/config"
	"github.com/kiran94/openitinerary-batch-processing/internal/di"
	"github.com/kiran94/openitinerary-batch-processing/internal/logging"
	"github.com/kiran94/openitinerary-batch-processing/pkg/telemetry"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
)

// Together these values will control how long it takes the server to shutdown
// in a worse case scenario so be careful when adjusting them.
// Within a k8 environment, the default grace period for a pod to shutdown is 30s
// so ensure this value is within that
// https://kubernetes.io/docs/concepts/workloads/pods/pod-lifecycle/#pod-termination-flow
const (
	shutdownServerTimeout = 15 * time.Second
)

func main() {
	grpcEnabled := flag.Bool("grpc", false, "enables the GRPC server")
	flag.Parse()

	if *grpcEnabled {
		if err := StartGRPCService(); err != nil {
			os.Exit(1)
		}
	} else {
		if err := StartHTTPService(); err != nil {
			os.Exit(1)
		}
	}
}

func StartHTTPService() error {
	ctx := context.Background()
	interruptCtx, interruptCancel := signal.NotifyContext(ctx, os.Interrupt, syscall.SIGTERM)
	defer interruptCancel()

	cfg := config.NewConfig()
	bootstrapLogger := logging.New(cfg)

	services, err := di.NewServices(ctx, cfg.GetOtelServiceName(), bootstrapLogger, cfg)
	if err != nil {
		return err
	}
	defer services.Shutdown()

	services.ServiceReadyManager.Start(interruptCtx)

	service := ServiceV1{
		config:        services.Config,
		logger:        services.Logger,
		metrics:       services.ServiceMetrics,
		tracer:        services.GetTracer("github.com/kiran94/openitinerary-batch-processing"),
		submitService: services.SubmitService,
		resultService: services.ResultService,
		statusService: services.StatusService,
	}

	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/v1/submit", service.submit)
	mux.HandleFunc("GET /api/v1/status/{id}", service.status)
	mux.HandleFunc("GET /api/v1/result/{id}", service.result)
	mux.HandleFunc("GET /health/live", services.ServiceReadyManager.Live())
	mux.HandleFunc("GET /health/ready", services.ServiceReadyManager.Ready())

	server := &http.Server{
		Handler: logging.RequestIDMiddleware(
			otelhttp.NewHandler(
				logging.RequestIDSpanMiddleware(mux), "api", otelhttp.WithFilter(telemetry.FilterRequests),
			),
		),
		Addr:         service.config.GetPort(),
		ReadTimeout:  service.config.GetServerReadTimeout(),
		WriteTimeout: service.config.GetServerWriteTimeout(),
		IdleTimeout:  service.config.GetServerIdleTimeout(),
	}

	services.Logger.Info(ctx,
		"server listening",
		"component", logging.ComponentHTTP,
		"listen_addr", server.Addr,
		"environment", services.Config.GetEnvironment(),
		"read_timeout", server.ReadTimeout.String(),
		"write_timeout", server.WriteTimeout.String(),
		"idle_timeout", server.IdleTimeout.String(),
	)

	errorCh := make(chan error, 1)
	go func() {
		errorCh <- server.ListenAndServe()
	}()

	select {
	case err := <-errorCh:
		if !errors.Is(err, http.ErrServerClosed) {
			services.Logger.Error(ctx, "server stopped unexpectedly", "component", logging.ComponentHTTP, "error", err)
			return err
		}
	case <-interruptCtx.Done():
		services.Logger.Info(ctx, "server cancellation was signaled", "component", logging.ComponentHTTP, "timeout_seconds", shutdownServerTimeout.Seconds())

		shutdownCtx := context.Background()
		shutdownCtx, shutdownCancel := context.WithTimeout(shutdownCtx, shutdownServerTimeout)
		defer shutdownCancel()

		services.ServiceReadyManager.Shutdown()
		if err := server.Shutdown(shutdownCtx); err != nil {
			services.Logger.Error(shutdownCtx, "error shutting down server", "component", logging.ComponentHTTP, "error", err)
			return err
		}
	}

	return nil
}

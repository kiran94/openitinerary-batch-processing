package health

import (
	"context"
	"database/sql"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"github.com/kiran94/openitinerary-batch-processing/internal/config"
	"github.com/kiran94/openitinerary-batch-processing/internal/logging"
)

type serviceReadyManager struct {
	sync.Mutex
	ready    bool
	shutdown bool
	logger   logging.Logger
	db       *sql.DB

	pollingInterval time.Duration
	pollingTimeout  time.Duration

	services []readyService
}

func NewServiceReadyManager(cfg config.Config, logger logging.Logger, db *sql.DB) *serviceReadyManager {
	return &serviceReadyManager{
		ready:           false,
		logger:          logger,
		db:              db,
		pollingInterval: cfg.GetServiceReadinessPollingInterval(),
		pollingTimeout:  cfg.GetServiceReadinessPollingTimeout(),
	}
}

func (srm *serviceReadyManager) Live() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}
}

func (srm *serviceReadyManager) Ready() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if srm.isReady() {
			w.WriteHeader(http.StatusOK)
		} else {
			w.WriteHeader(http.StatusServiceUnavailable)
		}
	}
}

// isReady returns the result of the latest run
func (srm *serviceReadyManager) isReady() bool {
	srm.Lock()
	defer srm.Unlock()
	return srm.ready
}

func (srm *serviceReadyManager) Start(ctx context.Context) {
	srm.services = []readyService{
		databaseReadyService{db: srm.db},
	}

	go func() {
		for {
			srm.logger.Debug(ctx, "starting readiness polling")
			srm.pollReady(ctx)

			select {
			case <-ctx.Done():
				srm.logger.Info(ctx, "received signal to stop health polling loop.")
				srm.updateReady(false)
				return
			case <-time.After(srm.pollingInterval):
			}
		}
	}()
}

func (srm *serviceReadyManager) pollReady(ctx context.Context) {
	ctx, cancelFunc := context.WithTimeout(ctx, srm.pollingTimeout)
	defer cancelFunc()

	// Fire all the ready checks concurrently and collect the results
	var wg sync.WaitGroup
	results := make([]bool, len(srm.services))
	for i, service := range srm.services {
		wg.Go(func() {
			if err := service.Ready(ctx); err != nil {
				srm.logger.Warn(ctx, "service was not ready", "dependency", string(service.Name()), "error", err)
				results[i] = false
			} else {
				results[i] = true
			}
		})
	}

	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		for _, result := range results {
			if !result {
				// if any of the services are not ready then update the state
				// and return immediately
				srm.updateReady(false)
				return
			}
		}

		srm.updateReady(true)

	case <-ctx.Done():
		srm.logger.Warn(ctx, "readiness check hit the deadline")
		srm.updateReady(false)
	}
}

func (srm *serviceReadyManager) updateReady(newValue bool, opts ...updateOption) {
	srm.Lock()
	defer srm.Unlock()

	if srm.shutdown {
		return
	}

	for _, o := range opts {
		o(srm)
	}

	srm.ready = newValue
}

// SHUTDOWN logic

type updateOption func(*serviceReadyManager)

func withShutdown() updateOption {
	return func(srm *serviceReadyManager) {
		srm.shutdown = true
	}
}

func (srm *serviceReadyManager) Shutdown() {
	srm.updateReady(false, withShutdown())
}

// DOWNSTREAM SERVICES

type readyService interface {
	Name() readinessService
	Ready(context.Context) error
}

type databaseReadyService struct {
	db *sql.DB
}

func (drs databaseReadyService) Name() readinessService {
	return readinessServiceDatabase
}

func (drs databaseReadyService) Ready(ctx context.Context) error {
	if err := drs.db.PingContext(ctx); err != nil {
		return err
	} else {
		return nil
	}
}

type readinessService string

const (
	readinessServiceDatabase readinessService = "database"
)

func (r readinessService) LogValue() slog.Value {
	return slog.StringValue(string(r))
}

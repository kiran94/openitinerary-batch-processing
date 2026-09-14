package health

import (
	"context"
	"database/sql"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/kiran94/openitinerary-batch-processing/internal/config"
	"github.com/kiran94/openitinerary-batch-processing/internal/logging"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLive(t *testing.T) {
	srm := NewServiceReadyManager(new(MockConfig), new(MockLogger), nil)
	server := httptest.NewServer(srm.Live())
	defer server.Close()

	client := server.Client()
	response, err := client.Get(server.URL)
	require.NoError(t, err)
	defer response.Body.Close()

	assert.Equal(t, http.StatusOK, response.StatusCode)
}

func createDatabase(t *testing.T) (*sql.DB, sqlmock.Sqlmock) {
	t.Helper()

	db, mock, err := sqlmock.New(sqlmock.MonitorPingsOption(true))
	require.NoError(t, err)

	return db, mock
}

func TestReady_DatabaseNotReady(t *testing.T) {
	tests := []struct {
		name           string
		pingError      error
		expectedStatus int
	}{
		{name: "all_services_ready", pingError: nil, expectedStatus: http.StatusOK},
		{name: "database_not_ready", pingError: errors.New("database not ready"), expectedStatus: http.StatusServiceUnavailable},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			db, mock := createDatabase(t)
			defer func() {
				if err := db.Close(); err != nil {
					t.Log(err)
				}
			}()

			if tt.pingError != nil {
				mock.ExpectPing().WillReturnError(tt.pingError)
			} else {
				mock.ExpectPing()
			}

			srm := NewServiceReadyManager(new(MockConfig), new(MockLogger), db)
			srm.Start(t.Context())
			defer srm.Shutdown()

			server := httptest.NewServer(srm.Ready())
			defer server.Close()

			client := server.Client()
			response, err := client.Get(server.URL)
			require.NoError(t, err)
			defer response.Body.Close()

			require.NoError(t, mock.ExpectationsWereMet())
			assert.Equal(t, tt.expectedStatus, response.StatusCode)
		})
	}
}

func TestReady_StickyShutdown(t *testing.T) {
	srm := NewServiceReadyManager(new(MockConfig), new(MockLogger), nil)

	srm.updateReady(true)
	require.True(t, srm.isReady())

	srm.Shutdown()

	// Future attempts to mark the service as ready
	srm.updateReady(true)
	srm.updateReady(true)

	assert.False(t, srm.isReady())
}

func TestReady_PollingCancel(t *testing.T) {
	db, mock := createDatabase(t)
	defer func() {
		_ = db.Close()
	}()
	mock.ExpectPing()

	ctx := context.Background()
	ctx, cancel := context.WithCancel(ctx)

	srm := NewServiceReadyManager(new(MockConfig), new(MockLogger), db)
	srm.Start(ctx)

	// Ensure the service is marked as ready before cancel
	require.Eventually(
		t,
		func() bool { return srm.isReady() == true },
		time.Second*1,
		time.Millisecond*25,
	)

	cancel()

	// Ensure the service is marked as not ready after cancel
	assert.Eventually(
		t,
		func() bool { return srm.isReady() == false },
		time.Second*1,
		time.Millisecond*25,
	)
}

var (
	_ config.Config  = (*MockConfig)(nil)
	_ logging.Logger = (*MockLogger)(nil)
)

type MockConfig struct {
	config.ApplicationConfig
}

func (*MockConfig) GetServiceReadinessPollingInterval() time.Duration {
	return 5 * time.Second
}

func (*MockConfig) GetServiceReadinessPollingTimeout() time.Duration {
	return time.Second
}

type MockLogger struct{}

func (*MockLogger) Debug(context.Context, string, ...any) {}
func (*MockLogger) Info(context.Context, string, ...any)  {}
func (*MockLogger) Warn(context.Context, string, ...any)  {}
func (*MockLogger) Error(context.Context, string, ...any) {}

func (m *MockLogger) WithContext(context.Context) logging.Logger { return m }

package worker

import (
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewWorkReportSetsStartTime(t *testing.T) {
	before := time.Now()

	report := NewWorkReport()

	after := time.Now()
	require.NotNil(t, report)
	assert.False(t, report.startTime.Before(before))
	assert.False(t, report.startTime.After(after))
}

func TestWorkReportMarksPollingTime(t *testing.T) {
	report := NewWorkReport()
	before := time.Now()

	report.MarkPollingTime()

	after := time.Now()
	assert.False(t, report.startPollingTime.Before(before))
	assert.False(t, report.startPollingTime.After(after))
}

func TestWorkReportMarksProcessingTime(t *testing.T) {
	report := NewWorkReport()
	before := time.Now()

	report.MarkProcessingTime()

	after := time.Now()
	assert.False(t, report.startProcessingTime.Before(before))
	assert.False(t, report.startProcessingTime.After(after))
}

func TestWorkReportPollingDuration(t *testing.T) {
	startTime := time.Now()
	report := &workReport{
		startTime:        startTime,
		startPollingTime: startTime.Add(1500 * time.Millisecond),
	}

	assert.Equal(t, int64(1500), report.GetPollingDurationMs())
}

func TestWorkReportProcessingDuration(t *testing.T) {
	report := &workReport{startPollingTime: time.Now().Add(-2 * time.Second)}

	assert.InDelta(t, int64(2000), report.GetProcessingTimeMs(), 100)
}

func TestWorkReportOverallDuration(t *testing.T) {
	report := &workReport{startTime: time.Now().Add(-3 * time.Second)}

	assert.InDelta(t, int64(3000), report.GetOverallTimeMs(), 100)
}

func TestWorkReportMarksProcessedItem(t *testing.T) {
	report := NewWorkReport()
	assert.False(t, report.GetProcessedItem())

	report.MarkProcessedItem()

	assert.True(t, report.GetProcessedItem())
}

func TestWorkReportSetsError(t *testing.T) {
	report := NewWorkReport()
	expectedErr := errors.New("worker failed")
	require.NoError(t, report.GetError())

	report.SetError(expectedErr)

	assert.ErrorIs(t, report.GetError(), expectedErr)
}

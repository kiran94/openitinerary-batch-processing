package worker

import "time"

type workReport struct {
	startTime           time.Time
	startPollingTime    time.Time
	startProcessingTime time.Time
	processedItem       bool
	err                 error
}

func NewWorkReport() *workReport {
	return &workReport{startTime: time.Now()}
}

func (wr *workReport) MarkPollingTime() {
	wr.startPollingTime = time.Now()
}

func (wr *workReport) MarkProcessingTime() {
	wr.startProcessingTime = time.Now()
}

func (wr *workReport) MarkProcessedItem() {
	wr.processedItem = true
}

func (wr *workReport) SetError(err error) {
	wr.err = err
}

func (wr *workReport) GetPollingDurationMs() int64 {
	return wr.startPollingTime.Sub(wr.startTime).Milliseconds()
}

func (wr *workReport) GetProcessingTimeMs() int64 {
	return time.Since(wr.startPollingTime).Milliseconds()
}

func (wr *workReport) GetOverallTimeMs() int64 {
	return time.Since(wr.startTime).Milliseconds()
}

func (wr *workReport) GetProcessedItem() bool {
	return wr.processedItem
}

func (wr *workReport) GetError() error {
	return wr.err
}

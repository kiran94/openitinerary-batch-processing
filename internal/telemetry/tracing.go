package telemetry

type TelemetryAttributes string

const (
	Traceparent TelemetryAttributes = "traceparent"
	Tracestate  TelemetryAttributes = "tracestate"
)

type TracingSpanName string

const (
	TracingSpanEnqueueJobs     TracingSpanName = "enqueue jobs"
	TracingSpanDequeueJobs     TracingSpanName = "dequeue jobs"
	TracingSpanDeleteJob       TracingSpanName = "delete job message"
	TracingSpanProcessJob      TracingSpanName = "process job"
	TracingSpanWorkerPolling   TracingSpanName = "worker polling"
	TracingSpanParseSubmission TracingSpanName = "parse submission"
	TracingSpanStoreAndEnqueue TracingSpanName = "store and enqueue job"
	TracingSpanWriteResponse   TracingSpanName = "write submission response"

	TracingSpanUploadDocument   TracingSpanName = "upload document"
	TracingSpanReadDocument     TracingSpanName = "read document"
	TracingSpanCreateResultLink TracingSpanName = "create result link"
	TracingSpanCreateTicket     TracingSpanName = "create ticket"
	TracingSpanGetTicket        TracingSpanName = "get ticket"
	TracingSpanUpdateTicket     TracingSpanName = "update ticket"
	TracingSpanConvertDocument  TracingSpanName = "convert document"
	TracingSpanSendNotification TracingSpanName = "send notification"
)

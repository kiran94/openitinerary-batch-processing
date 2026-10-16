package logging

import (
	"log/slog"
)

type Component string

const (
	ComponentDatabase  Component = "database"
	ComponentAdapter   Component = "adapter"
	ComponentHTTP      Component = "http"
	ComponentGRPC      Component = "grpc"
	ComponentQueue     Component = "queue"
	ComponentStorage   Component = "storage"
	ComponentWorker    Component = "worker"
	ComponentNotifier  Component = "notifier"
	ComponentTelemetry Component = "telemetry"
)

func (c Component) LogValue() slog.Value {
	return slog.StringValue(string(c))
}

type Operation string

const (
	OperationSubmit                Operation = "submit"
	OperationStatus                Operation = "status"
	OperationResultLink            Operation = "result_link"
	OperationEnqueue               Operation = "enqueue"
	OperationDequeue               Operation = "dequeue"
	OperationDeleteMessage         Operation = "delete_message"
	OperationMarkFailed            Operation = "mark_failed"
	OperationStorageRead           Operation = "storage_read"
	OperationStorageWrite          Operation = "storage_write"
	OperationStoragePresign        Operation = "storage_presign"
	OperationAdapterConvert        Operation = "adapter_convert"
	OperationNotificationSend      Operation = "notification_send"
	OperationTicketCreate          Operation = "ticket_create"
	OperationTicketLookup          Operation = "ticket_lookup"
	OperationTicketUpdate          Operation = "ticket_update"
	OperationDuplicateDelivery     Operation = "duplicate_delivery"
	OperationMultipartCleanup      Operation = "multipart_cleanup"
	OperationTelemetryError        Operation = "telemetry_error"
	OperationResponseSerialization Operation = "response_serialization"
	OperationErrorResponseWrite    Operation = "response_write"
	OperationErrorWriteFailed      Operation = "error_response_write"
)

func (o Operation) LogValue() slog.Value {
	return slog.StringValue(string(o))
}

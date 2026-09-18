package metrics

import (
	"context"

	"github.com/kiran94/openitinerary-batch-processing/internal/config"
)

type SubmissionRecorder interface {
	/*
		Called when a Submission to the API has occurred
		contributes to metrics:

		* oibp.api.submissions
		* oibp.jobs.enqueued
		* oibp.document.input.size
	*/
	RecordSubmission(context.Context, SubmissionObservation)
}

type SubmissionObservation struct {
	Outcome     SubmissionOutcome
	Reason      *SubmissionReason
	ContentType *config.ContentType
	InputBytes  *int64
	Enqueued    bool
	ServiceType SubmissionServiceType
}

type SubmissionOutcome string

const (
	SubmissionAccepted SubmissionOutcome = "accepted"
	SubmissionRejected SubmissionOutcome = "rejected"
	SubmissionFailed   SubmissionOutcome = "failed"
)

type SubmissionReason string

const (
	SubmissionReasonInvalidMultipartForm        SubmissionReason = "invalid_multipart_form"
	SubmissionReasonRequestBodyTooLarge         SubmissionReason = "request_body_too_large"
	SubmissionReasonFileNotProvided             SubmissionReason = "file_not_provided"
	SubmissionReasonContentTypeNotProvided      SubmissionReason = "content_type_not_provided"
	SubmissionReasonInvalidContentType          SubmissionReason = "invalid_content_type"
	SubmissionReasonInvalidDocumentInput        SubmissionReason = "invalid_document_input"
	SubmissionReasonInvalidNotificationEmails   SubmissionReason = "invalid_notification_emails"
	SubmissionReasonUnsupportedContentType      SubmissionReason = "unsupported_content_type"
	SubmissionReasonStorageWriteFailed          SubmissionReason = "storage_write_failed"
	SubmissionReasonTicketCreationFailed        SubmissionReason = "ticket_creation_failed"
	SubmissionReasonQueueEnqueueFailed          SubmissionReason = "queue_enqueue_failed"
	SubmissionReasonResponseSerializationFailed SubmissionReason = "response_serialization_failed"
)

type SubmissionServiceType string

const (
	SubmissionServiceTypeHTTP = "http"
	SubmissionServiceTypeGRPC = "grpc"
)

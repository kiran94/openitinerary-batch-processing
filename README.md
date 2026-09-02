# OpenItinerary Batch Processing

## Architecture

![io_batch_processing](./docs/io_batch_processing.png)

## TODO

Sending User Notification
Presigned URL for result

Atomic Job creation (When a new job comes in, if any failure occurs then all operations are rolled back to keep the state clean)

Add Metric Collection
    Adapter Result Size
    S3 Upload Duration
    End-to-End Job Duration
    Worker RSS (Resident Set Size)

Reprocessing Job
GRPC

Add Error/Alerting
Add OpenTelemetry
Add Authentication / Authorization
Add UI for overview
Deploy to K8 Locally
Upgrade to Go 1.17

Cleanup Processes


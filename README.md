# OpenItinerary Batch Processing

## Architecture

![io_batch_processing](./docs/io_batch_processing.png)


## Features


## Observability

This project includes observaility of the following structure:


```
Go API / Worker → OTLP → OpenTelemetry Collector → Prometheus → Grafana
                                             later → Tempo
                                             later → Loki
```

### Metrics

We collect the following metrics from the API and Worker.

| Metric | Type | Unit | Attributes |
|---|---|---:|---|
| `http.server.request.duration` | Histogram | `s` | Standard OTel method, route and status attributes |
| `oibp.api.submissions` | Counter | `1` | `outcome`, optional `reason` |
| `oibp.api.response_write.errors` | Counter | `1` | `route` |
| `oibp.jobs.enqueued` | Counter | `1` | `content_type` |
| `oibp.jobs.terminal` | Counter | `1` | `outcome`, optional `status_reason` |
| `oibp.worker.runs` | Counter | `1` | `outcome` |
| `oibp.document.input.size` | Histogram | `By` | `content_type` |
| `oibp.adapter.convert.duration` | Histogram | `s` | `outcome` |
| `oibp.adapter.response.size` | Histogram | `By` | `content_type` |
| `oibp.storage.upload.duration` | Histogram | `s` | `purpose`, `outcome` |
| `oibp.worker.polling.duration` | Histogram | `s` | `outcome` |
| `oibp.worker.processing.duration` | Histogram | `s` | `outcome` |
| `oibp.job.end_to_end.duration` | Histogram | `s` | `outcome`, optional `status_reason` |



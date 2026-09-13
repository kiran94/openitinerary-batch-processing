# k6 endpoint load tests

These scripts intentionally contain one sustained load test per endpoint:

- `status-constant.js` — `GET /api/v1/status/{id}` at 5 requests/second
- `result-constant.js` — `GET /api/v1/result/{id}` at 5 requests/second
- `submit-constant.js` — `POST /api/v1/submit` at 1 request/second
- `submit-rejected.js` — one negative-request contract check

The baseline and ramp variants were removed because they duplicated the endpoint
tests without providing a better p95 signal. The constant-arrival tests produce
the traffic; the API's request-duration metrics are used for Grafana analysis.

Run the read-endpoint tests with an existing ticket ID:

```bash
TICKET_ID=<ticket-id> k6 run infra/k6/status-constant.js
TICKET_ID=<ticket-id> k6 run infra/k6/result-constant.js
```

Run the submit load test with the sample PDF expected by the script:

```bash
k6 run infra/k6/submit-constant.js
```

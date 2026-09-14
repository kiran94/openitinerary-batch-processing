# k6 endpoint load tests

This repository uses [k6](https://grafana.com/docs/k6/latest/) for load testing and performance verification.

```bash
# Check the status endpoint
TICKET_ID=EXISTING_TICKET k6 run infra/k6/status-constant.js

# Check the result endpoint
TICKET_ID=COMPLETED_TICKET k6 run infra/k6/result-constant.js

# Check the submit endpoint
# Note that this endpoint will generate pending work for the workers
# so if you don't want workers to start executing all the work then remove them from the queue/database
k6 run infra/k6/submit-constant.js
```

## Data File

For the `submit` endpoint load tests, you need to put a PDF file locally so it can be loaded into memory and simulate a document submission. Everyone should use the same PDF so that metrics are comparable therefore the base PDF file has been uploaded in the AWS bucket. Download it via:

```bash
aws s3 cp s3://openitinerary-batch-processing/k6/data/test.pdf infra/k6/data/test.pdf
```

resource "aws_sqs_queue" "job_queue" {
  name                       = "openitinerary-batch-processing-job-queue.fifo"
  fifo_queue                 = true   # used for the deduplication features
  visibility_timeout_seconds = 360    # 6 mins
  message_retention_seconds  = 259200 # 3 Days

  redrive_policy = jsonencode({
    deadLetterTargetArn = aws_sqs_queue.job_queue_dlq.arn
    maxReceiveCount     = 3
  })
}

resource "aws_sqs_queue" "job_queue_dlq" {
  name       = "openitinerary-batch-processing-job-queue-dlq.fifo"
  fifo_queue = true
}

output "job_queue" {
  value = aws_sqs_queue.job_queue.url
}

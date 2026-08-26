resource "aws_sqs_queue" "job_queue" {
  name                       = "openitinerary-batch-processing-job-queue.fifo"
  fifo_queue                 = true # used for the deduplication features
  visibility_timeout_seconds = 30
  message_retention_seconds  = 259200 # 3 Days
}

output "job_queue" {
  value = aws_sqs_queue.job_queue.url
}

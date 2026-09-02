variable "notification_sender_email" {
  description = "Verified email address used to send job status notifications."
  type        = string
  nullable    = false

  validation {
    condition     = can(regex("^[^[:space:]@]+@[^[:space:]@]+\\.[^[:space:]@]+$", var.notification_sender_email))
    error_message = "notification_sender_email must be a valid email address."
  }
}

resource "aws_sesv2_configuration_set" "notifications" {
  configuration_set_name = "openitinerary-batch-processing-notifications"

  reputation_options {
    reputation_metrics_enabled = true
  }

  sending_options {
    sending_enabled = true
  }

  suppression_options {
    suppressed_reasons = ["BOUNCE", "COMPLAINT"]
  }

  tags = {
    Application = "openitinerary-batch-processing"
    Component   = "notification"
  }
}

resource "aws_sesv2_email_identity" "notification_sender" {
  email_identity         = var.notification_sender_email
  configuration_set_name = aws_sesv2_configuration_set.notifications.configuration_set_name

  tags = {
    Application = "openitinerary-batch-processing"
    Component   = "notification"
  }
}

output "notification_configuration_set_name" {
  description = "SES configuration set used for job status notifications."
  value       = aws_sesv2_configuration_set.notifications.configuration_set_name
}

output "notification_sender_identity_arn" {
  description = "ARN of the SES sender identity."
  value       = aws_sesv2_email_identity.notification_sender.arn
}

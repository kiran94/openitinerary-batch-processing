resource "aws_s3_bucket" "main" {
  bucket = "openitinerary-batch-processing"
}

resource "aws_s3_bucket_lifecycle_configuration" "jobs_lifecycle" {
  bucket                                 = aws_s3_bucket.main.bucket
  transition_default_minimum_object_size = "all_storage_classes_128K"

  rule {
    id     = "transition-jobs-standard-ia"
    status = "Enabled"
    filter {
      prefix = "jobs/"
    }

    transition {
      days          = 5
      storage_class = "STANDARD_IA"
    }
  }

  rule {
    id     = "transition-jobs-glacier-ir"
    status = "Enabled"
    filter {
      prefix = "jobs/"
    }

    transition {
      days          = 35
      storage_class = "GLACIER_IR"
    }
  }
}

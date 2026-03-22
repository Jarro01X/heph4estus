variable "name_prefix" {
  description = "Prefix for resource names"
  type        = string
}

variable "environment" {
  description = "Environment name (dev, staging, prod)"
  type        = string
}

variable "sqs_queue_arn" {
  description = "ARN of the SQS queue for worker access"
  type        = string
}

variable "s3_bucket_arn" {
  description = "ARN of the S3 bucket for result uploads"
  type        = string
}

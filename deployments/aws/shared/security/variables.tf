variable "name_prefix" {
  description = "Prefix for resource names"
  type        = string
  default     = "nmap-scanner"
}

variable "environment" {
  description = "Environment name"
  type        = string
  default     = "dev"
}

variable "sqs_queue_arn" {
  description = "ARN of the SQS queue for tasks"
  type        = string
}

variable "s3_bucket_arn" {
  description = "ARN of the S3 bucket for results"
  type        = string
}
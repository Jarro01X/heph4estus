variable "name_prefix" {
  description = "Prefix for resource names"
  type        = string
}

variable "environment" {
  description = "Environment name"
  type        = string
  default     = "dev"
}

variable "force_destroy_bucket" {
  description = "Whether to force destroy the S3 bucket even if it contains objects"
  type        = bool
  default     = false
}

variable "kms_key_arn" {
  description = "ARN of the customer-managed KMS key used for result bucket encryption"
  type        = string
}

variable "results_retention_days" {
  description = "Number of days to retain results before deletion"
  type        = number
  default     = 90
}

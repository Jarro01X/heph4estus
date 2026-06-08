variable "name_prefix" {
  description = "Prefix for resource names"
  type        = string
}

variable "environment" {
  description = "Environment name"
  type        = string
  default     = "dev"
}

variable "kms_key_arn" {
  description = "ARN of the customer-managed KMS key used for queue encryption"
  type        = string
}

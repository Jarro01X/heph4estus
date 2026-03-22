variable "name_prefix" {
  description = "Prefix for resource names"
  type        = string
}

variable "environment" {
  description = "Environment name"
  type        = string
  default     = "dev"
}

variable "aws_region" {
  description = "AWS region"
  type        = string
}

variable "log_retention_days" {
  description = "Number of days to retain CloudWatch logs"
  type        = number
  default     = 30
}

variable "task_cpu" {
  description = "CPU units for the ECS task"
  type        = number
  default     = 256
}

variable "task_memory" {
  description = "Memory for the ECS task (MB)"
  type        = number
  default     = 512
}

variable "ecs_execution_role_arn" {
  description = "ARN of the IAM role for ECS task execution"
  type        = string
}

variable "ecs_task_role_arn" {
  description = "ARN of the IAM role for ECS task"
  type        = string
}

variable "sqs_queue_url" {
  description = "URL of the SQS queue for tasks"
  type        = string
}

variable "s3_bucket_id" {
  description = "ID of the S3 bucket for results"
  type        = string
}

variable "tool_name" {
  description = "Name of the tool module to run (e.g. nmap, nuclei, ffuf)"
  type        = string
}

variable "jitter_max_seconds" {
  description = "Maximum jitter delay before each task (0 = disabled)"
  type        = number
  default     = 0
}

variable "container_env_vars" {
  description = "Additional environment variables for the container"
  type        = map(string)
  default     = {}
}

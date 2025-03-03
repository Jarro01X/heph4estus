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

variable "step_functions_role_arn" {
  description = "ARN of the IAM role for Step Functions"
  type        = string
}

variable "sqs_queue_url" {
  description = "URL of the SQS queue for tasks"
  type        = string
}

variable "ecs_cluster_arn" {
  description = "ARN of the ECS cluster"
  type        = string
}

variable "ecs_task_definition_arn" {
  description = "ARN of the ECS task definition"
  type        = string
}

variable "private_subnet_ids" {
  description = "IDs of the private subnets"
  type        = list(string)
}

variable "ecs_security_group_id" {
  description = "ID of the security group for ECS tasks"
  type        = string
}

variable "log_retention_days" {
  description = "Number of days to retain CloudWatch logs"
  type        = number
  default     = 30
}

variable "max_concurrency" {
  description = "Maximum number of concurrent executions in the Map state"
  type        = number
  default     = 10
}

variable "task_timeout_seconds" {
  description = "Timeout for ECS tasks in seconds"
  type        = number
  default     = 480 # 8 minutes
}
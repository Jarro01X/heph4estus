variable "aws_region" {
  description = "AWS region to deploy resources"
  type        = string
  default     = "us-east-1"
}

variable "name_prefix" {
  description = "Prefix for all resource names"
  type        = string
  default     = "heph-dev"
}

variable "tool_name" {
  description = "Name of the tool module to run (e.g. nmap, nuclei, ffuf)"
  type        = string
}

variable "vpc_cidr" {
  description = "CIDR block for VPC"
  type        = string
  default     = "10.0.0.0/16"
}

variable "az_count" {
  description = "Number of availability zones to use"
  type        = number
  default     = 2
}

variable "log_retention_days" {
  description = "Number of days to retain logs"
  type        = number
  default     = 30
}

variable "task_cpu" {
  description = "CPU units for ECS tasks"
  type        = number
  default     = 256
}

variable "task_memory" {
  description = "Memory for ECS tasks (MB)"
  type        = number
  default     = 512
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

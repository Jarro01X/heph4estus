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

variable "enable_ipv6" {
  description = "Enable dual-stack IPv6 networking with egress-only internet gateway for private subnet IPv6 egress"
  type        = bool
  default     = false
}

variable "multi_nat" {
  description = "Create one NAT gateway per availability zone for IPv4 source IP diversity"
  type        = bool
  default     = false
}

variable "kms_key_arn" {
  description = "ARN of the customer-managed KMS key used by encrypted AWS networking logs"
  type        = string
}

variable "log_retention_days" {
  description = "Number of days to retain VPC Flow Logs"
  type        = number
  default     = 30
}

variable "scanner_egress_ipv4_cidr_blocks" {
  description = "IPv4 CIDR blocks scanner workers may egress to"
  type        = list(string)
  default     = ["0.0.0.0/0"]
}

variable "scanner_egress_ipv6_cidr_blocks" {
  description = "IPv6 CIDR blocks scanner workers may egress to when IPv6 is enabled"
  type        = list(string)
  default     = ["::/0"]
}

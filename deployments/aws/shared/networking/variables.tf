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

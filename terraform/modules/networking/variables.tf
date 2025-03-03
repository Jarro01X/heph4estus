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
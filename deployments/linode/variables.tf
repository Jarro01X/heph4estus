variable "linode_token" {
  description = "Linode API token."
  type        = string
  sensitive   = true
}

variable "tool_name" {
  description = "Tool name for lifecycle detection."
  type        = string
}

variable "worker_count" {
  description = "Number of worker VMs."
  type        = number
  default     = 3
}

variable "controller_type" {
  description = "Linode instance type for controller (e.g. g6-standard-1)."
  type        = string
  default     = "g6-standard-1"
}

variable "worker_type" {
  description = "Linode instance type for workers (e.g. g6-nanode-1)."
  type        = string
  default     = "g6-nanode-1"
}

variable "region" {
  description = "Linode region (e.g. us-east, eu-west)."
  type        = string
  default     = "us-east"
}

variable "ssh_public_key" {
  description = "SSH public key for VM access."
  type        = string
}

variable "minio_bucket" {
  description = "MinIO bucket name."
  type        = string
  default     = "heph-results"
}

variable "docker_image" {
  description = "Worker Docker image (relative to controller registry)."
  type        = string
  default     = "heph-worker:latest"
}

variable "generation_id" {
  description = "Fleet generation ID for ownership tracking."
  type        = string
  default     = ""
}

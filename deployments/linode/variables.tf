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

variable "controller_security_mode" {
  description = "Controller service security mode. private-auth is the current compatibility mode; tls and mtls are reserved for hardened controller service transport."
  type        = string
  default     = "private-auth"

  validation {
    condition     = contains(["private-auth", "tls", "mtls"], var.controller_security_mode)
    error_message = "controller_security_mode must be one of: private-auth, tls, mtls."
  }
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

variable "nats_operator_user_override" {
  description = "Rotated NATS operator username. Empty uses the controller module bootstrap credential."
  type        = string
  default     = ""
}

variable "nats_operator_password_override" {
  description = "Rotated NATS operator password. Empty uses the controller module bootstrap credential."
  type        = string
  default     = ""
  sensitive   = true
}

variable "nats_worker_user_override" {
  description = "Rotated NATS worker username. Empty uses the controller module bootstrap credential."
  type        = string
  default     = ""
}

variable "nats_worker_password_override" {
  description = "Rotated NATS worker password. Empty uses the controller module bootstrap credential."
  type        = string
  default     = ""
  sensitive   = true
}

variable "nats_credential_generation" {
  description = "NATS credential generation marker for operator-driven rotation."
  type        = string
  default     = "bootstrap"
}

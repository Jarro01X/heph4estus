variable "hcloud_token" {
  description = "Hetzner Cloud API token."
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
  description = "Hetzner server type for controller."
  type        = string
  default     = "cx22"
}

variable "worker_type" {
  description = "Hetzner server type for workers."
  type        = string
  default     = "cx22"
}

variable "location" {
  description = "Hetzner datacenter location."
  type        = string
  default     = "fsn1"
}

variable "ssh_public_key" {
  description = "SSH public key for VM access."
  type        = string
}

variable "ssh_key_name" {
  description = "Name for the SSH key resource."
  type        = string
  default     = "heph-deploy"
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

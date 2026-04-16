variable "vultr_api_key" {
  description = "Vultr API key."
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

variable "controller_plan" {
  description = "Vultr plan for the controller (e.g. vc2-1c-2gb)."
  type        = string
  default     = "vc2-1c-2gb"
}

variable "worker_plan" {
  description = "Vultr plan for workers (e.g. vc2-1c-1gb)."
  type        = string
  default     = "vc2-1c-1gb"
}

variable "region" {
  description = "Vultr region (e.g. ewr, lax, fra)."
  type        = string
  default     = "ewr"
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

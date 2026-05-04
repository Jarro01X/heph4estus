variable "controller_ip" {
  description = "Public IP address of the controller VM. Used to construct service endpoint URLs in outputs."
  type        = string
}

variable "tool_name" {
  description = "Tool name for lifecycle mismatch detection (passed through to outputs)."
  type        = string
}

variable "minio_bucket" {
  description = "Default MinIO bucket name, created during cloud-init bootstrap."
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

variable "controller_cert_validity_hours" {
  description = "Validity period for generated controller CA and server certificates."
  type        = number
  default     = 8760
}

variable "nats_port" {
  description = "NATS client port."
  type        = number
  default     = 4222
}

variable "nats_monitor_port" {
  description = "NATS HTTP monitoring port."
  type        = number
  default     = 8222
}

variable "nats_stream_name" {
  description = "NATS JetStream stream name for task distribution."
  type        = string
  default     = "heph-tasks"
}

variable "minio_port" {
  description = "MinIO S3 API port."
  type        = number
  default     = 9000
}

variable "minio_console_port" {
  description = "MinIO web console port."
  type        = number
  default     = 9001
}

variable "registry_port" {
  description = "Docker registry port."
  type        = number
  default     = 5000
}

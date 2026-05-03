# Selfhosted controller module — generates cloud-init bootstrap and credentials
# for a controller VM running NATS JetStream, MinIO, and a Docker registry.
#
# This module does NOT provision the VM itself. Provider-specific modules
# (e.g. deployments/hetzner/) compose this module and pass its cloud_init
# output as the VM's user_data. This keeps the controller logic portable
# across any VPS provider that supports cloud-init.
#
# PR 6.3 will add the first provider-specific composition (Hetzner).

terraform {
  required_version = ">= 1.3"
}

# --- Credential generation ---

resource "random_password" "minio_secret_key" {
  length  = 40
  special = false
}

resource "random_id" "minio_access_key" {
  byte_length = 10
}

resource "random_password" "nats_password" {
  length  = 32
  special = false
}

# --- Cloud-init rendering ---

locals {
  nats_user                = "heph"
  nats_tls_enabled         = contains(["tls", "mtls"], var.controller_security_mode)
  minio_tls_enabled        = contains(["tls", "mtls"], var.controller_security_mode)
  registry_tls_enabled     = contains(["tls", "mtls"], var.controller_security_mode)
  registry_auth_enabled    = false
  nats_auth_enabled        = true
  minio_auth_enabled       = true
  controller_security_mode = var.controller_security_mode

  cloud_init = templatefile("${path.module}/templates/cloud-init.yaml", {
    minio_access_key   = random_id.minio_access_key.hex
    minio_secret_key   = random_password.minio_secret_key.result
    minio_bucket       = var.minio_bucket
    minio_port         = var.minio_port
    minio_console_port = var.minio_console_port
    nats_port          = var.nats_port
    nats_monitor_port  = var.nats_monitor_port
    nats_stream_name   = var.nats_stream_name
    registry_port      = var.registry_port
    nats_user          = local.nats_user
    nats_password      = random_password.nats_password.result
  })
}

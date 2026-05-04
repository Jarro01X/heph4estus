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

  required_providers {
    tls = {
      source  = "hashicorp/tls"
      version = ">= 4.0"
    }
  }
}

# --- Credential generation ---

resource "random_password" "minio_secret_key" {
  length  = 40
  special = false
}

resource "random_id" "minio_access_key" {
  byte_length = 10
}

resource "random_password" "minio_operator_secret_key" {
  length  = 40
  special = false
}

resource "random_id" "minio_operator_access_key" {
  byte_length = 10
}

resource "random_password" "minio_worker_secret_key" {
  length  = 40
  special = false
}

resource "random_id" "minio_worker_access_key" {
  byte_length = 10
}

resource "random_password" "nats_password" {
  length  = 32
  special = false
}

resource "random_password" "nats_worker_password" {
  length  = 32
  special = false
}

resource "random_password" "registry_publisher_password" {
  length  = 32
  special = false
}

resource "random_password" "registry_worker_password" {
  length  = 32
  special = false
}

# --- TLS material ---

resource "tls_private_key" "controller_ca" {
  algorithm   = "ECDSA"
  ecdsa_curve = "P256"
}

resource "tls_self_signed_cert" "controller_ca" {
  private_key_pem       = tls_private_key.controller_ca.private_key_pem
  is_ca_certificate     = true
  validity_period_hours = var.controller_cert_validity_hours

  allowed_uses = [
    "cert_signing",
    "crl_signing",
    "digital_signature",
    "key_encipherment",
  ]

  subject {
    common_name  = "heph4estus controller CA"
    organization = "heph4estus"
  }
}

resource "tls_private_key" "controller_server" {
  algorithm   = "ECDSA"
  ecdsa_curve = "P256"
}

resource "tls_cert_request" "controller_server" {
  private_key_pem = tls_private_key.controller_server.private_key_pem
  dns_names       = ["heph-controller", "localhost", "host.docker.internal"]
  ip_addresses    = concat(["127.0.0.1"], var.controller_ip != "" && var.controller_ip != "0.0.0.0" ? [var.controller_ip] : [])

  subject {
    common_name  = "heph-controller"
    organization = "heph4estus"
  }
}

resource "tls_locally_signed_cert" "controller_server" {
  cert_request_pem      = tls_cert_request.controller_server.cert_request_pem
  ca_private_key_pem    = local.controller_ca_key_pem
  ca_cert_pem           = local.controller_ca_pem
  validity_period_hours = var.controller_cert_validity_hours

  allowed_uses = [
    "digital_signature",
    "key_encipherment",
    "server_auth",
  ]
}

resource "tls_private_key" "nats_operator_client" {
  algorithm   = "ECDSA"
  ecdsa_curve = "P256"
}

resource "tls_cert_request" "nats_operator_client" {
  private_key_pem = tls_private_key.nats_operator_client.private_key_pem

  subject {
    common_name  = "heph-nats-operator"
    organization = "heph4estus"
  }
}

resource "tls_locally_signed_cert" "nats_operator_client" {
  cert_request_pem      = tls_cert_request.nats_operator_client.cert_request_pem
  ca_private_key_pem    = local.controller_ca_key_pem
  ca_cert_pem           = local.controller_ca_pem
  validity_period_hours = var.controller_cert_validity_hours

  allowed_uses = [
    "digital_signature",
    "key_encipherment",
    "client_auth",
  ]
}

resource "tls_private_key" "nats_worker_client" {
  algorithm   = "ECDSA"
  ecdsa_curve = "P256"
}

resource "tls_cert_request" "nats_worker_client" {
  private_key_pem = tls_private_key.nats_worker_client.private_key_pem

  subject {
    common_name  = "heph-nats-worker"
    organization = "heph4estus"
  }
}

resource "tls_locally_signed_cert" "nats_worker_client" {
  cert_request_pem      = tls_cert_request.nats_worker_client.cert_request_pem
  ca_private_key_pem    = local.controller_ca_key_pem
  ca_cert_pem           = local.controller_ca_pem
  validity_period_hours = var.controller_cert_validity_hours

  allowed_uses = [
    "digital_signature",
    "key_encipherment",
    "client_auth",
  ]
}

# --- Cloud-init rendering ---

locals {
  credential_scope_version = "nats-minio-registry-role-v1"
  nats_operator_user       = "heph-operator"
  nats_worker_user         = "heph-worker"
  registry_publisher_user  = "heph-registry-publisher"
  registry_worker_user     = "heph-registry-worker"
  nats_user                = local.nats_operator_user
  nats_tls_enabled         = contains(["tls", "mtls"], var.controller_security_mode)
  nats_mtls_enabled        = var.controller_security_mode == "mtls"
  minio_tls_enabled        = contains(["tls", "mtls"], var.controller_security_mode)
  registry_tls_enabled     = contains(["tls", "mtls"], var.controller_security_mode)
  registry_auth_enabled    = true
  nats_auth_enabled        = true
  minio_auth_enabled       = true
  controller_security_mode = var.controller_security_mode
  controller_host          = "heph-controller"
  nats_scheme              = local.nats_tls_enabled ? "tls" : "nats"
  minio_scheme             = local.minio_tls_enabled ? "https" : "http"
  registry_scheme          = local.registry_tls_enabled ? "https" : "http"
  controller_ca_pem        = var.controller_ca_pem_override != "" ? var.controller_ca_pem_override : tls_self_signed_cert.controller_ca.cert_pem
  controller_ca_key_pem    = var.controller_ca_key_pem_override != "" ? var.controller_ca_key_pem_override : tls_private_key.controller_ca.private_key_pem
  controller_cert_pem      = var.controller_cert_pem_override != "" ? var.controller_cert_pem_override : tls_locally_signed_cert.controller_server.cert_pem
  controller_key_pem       = var.controller_key_pem_override != "" ? var.controller_key_pem_override : tls_private_key.controller_server.private_key_pem
  controller_cert_not_after = (
    var.controller_cert_not_after_override != "" ? var.controller_cert_not_after_override : tls_locally_signed_cert.controller_server.validity_end_time
  )
  controller_ca_fingerprint_sha256 = sha256(local.controller_ca_pem)

  cloud_init = templatefile("${path.module}/templates/cloud-init.yaml", {
    minio_root_access_key       = random_id.minio_access_key.hex
    minio_root_secret_key       = random_password.minio_secret_key.result
    minio_operator_access_key   = random_id.minio_operator_access_key.hex
    minio_operator_secret_key   = random_password.minio_operator_secret_key.result
    minio_worker_access_key     = random_id.minio_worker_access_key.hex
    minio_worker_secret_key     = random_password.minio_worker_secret_key.result
    minio_bucket                = var.minio_bucket
    minio_port                  = var.minio_port
    minio_console_port          = var.minio_console_port
    nats_port                   = var.nats_port
    nats_monitor_port           = var.nats_monitor_port
    nats_stream_name            = var.nats_stream_name
    registry_port               = var.registry_port
    registry_publisher_user     = local.registry_publisher_user
    registry_publisher_password = random_password.registry_publisher_password.result
    registry_worker_user        = local.registry_worker_user
    registry_worker_password    = random_password.registry_worker_password.result
    nats_operator_user          = local.nats_operator_user
    nats_operator_password      = random_password.nats_password.result
    nats_worker_user            = local.nats_worker_user
    nats_worker_password        = random_password.nats_worker_password.result
    tls_enabled                 = local.nats_tls_enabled
    nats_mtls_enabled           = local.nats_mtls_enabled
    nats_scheme                 = local.nats_scheme
    minio_scheme                = local.minio_scheme
    controller_ca_pem_b64       = base64encode(local.controller_ca_pem)
    controller_cert_pem_b64     = base64encode(local.controller_cert_pem)
    controller_key_pem_b64      = base64encode(local.controller_key_pem)
    nats_operator_cert_pem_b64  = base64encode(tls_locally_signed_cert.nats_operator_client.cert_pem)
    nats_operator_key_pem_b64   = base64encode(tls_private_key.nats_operator_client.private_key_pem)
  })
}

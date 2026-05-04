# Output names map onto the selfhosted env family so that future PR 6.3
# deploy UX can wire these directly into the operator's environment.
#
# Sensitive outputs are marked so Terraform does not display them in plan
# or apply output. The infra.RedactOutputs helper provides a second layer
# when reading outputs programmatically.

output "tool_name" {
  description = "Tool name (passed through for lifecycle mismatch detection)."
  value       = var.tool_name
}

output "nats_url" {
  description = "NATS client URL for workers and the operator CLI."
  value       = "${local.nats_scheme}://${var.controller_ip}:${var.nats_port}"
}

output "controller_security_mode" {
  description = "Controller service security mode."
  value       = local.controller_security_mode
}

output "nats_tls_enabled" {
  description = "Whether the NATS client listener is configured for TLS."
  value       = local.nats_tls_enabled
}

output "nats_auth_enabled" {
  description = "Whether the NATS client listener requires authentication."
  value       = local.nats_auth_enabled
}

output "minio_tls_enabled" {
  description = "Whether the MinIO S3 API is configured for TLS."
  value       = local.minio_tls_enabled
}

output "minio_auth_enabled" {
  description = "Whether MinIO requires credentials."
  value       = local.minio_auth_enabled
}

output "registry_tls_enabled" {
  description = "Whether the controller registry is configured for TLS."
  value       = local.registry_tls_enabled
}

output "registry_auth_enabled" {
  description = "Whether the controller registry requires authentication."
  value       = local.registry_auth_enabled
}

output "controller_ca_pem" {
  description = "PEM-encoded controller CA certificate used to trust TLS endpoints."
  value       = tls_self_signed_cert.controller_ca.cert_pem
}

output "controller_ca_fingerprint_sha256" {
  description = "SHA-256 fingerprint of the controller CA PEM."
  value       = sha256(tls_self_signed_cert.controller_ca.cert_pem)
}

output "controller_cert_not_after" {
  description = "RFC3339 expiration timestamp for the controller server certificate."
  value       = tls_locally_signed_cert.controller_server.validity_end_time
}

output "controller_host" {
  description = "Stable DNS name workers map to the controller private IP."
  value       = local.controller_host
}

output "nats_stream" {
  description = "NATS JetStream stream name."
  value       = var.nats_stream_name
}

output "s3_endpoint" {
  description = "MinIO S3-compatible endpoint URL."
  value       = "${local.minio_scheme}://${var.controller_ip}:${var.minio_port}"
}

output "s3_region" {
  description = "S3 region (MinIO ignores this but clients require it)."
  value       = "us-east-1"
}

output "s3_access_key" {
  description = "MinIO root access key."
  value       = random_id.minio_access_key.hex
  sensitive   = true
}

output "s3_secret_key" {
  description = "MinIO root secret key."
  value       = random_password.minio_secret_key.result
  sensitive   = true
}

output "s3_path_style" {
  description = "Whether to use path-style S3 access (always true for MinIO)."
  value       = "true"
}

output "s3_bucket_name" {
  description = "Default storage bucket name."
  value       = var.minio_bucket
}

output "registry_url" {
  description = "Docker registry URL for worker image distribution."
  value       = "${local.registry_scheme}://${var.controller_ip}:${var.registry_port}"
}

output "nats_user" {
  description = "NATS authentication username."
  value       = local.nats_user
}

output "nats_password" {
  description = "NATS authentication password."
  value       = random_password.nats_password.result
  sensitive   = true
}

output "cloud_init" {
  description = "Rendered cloud-init content for the controller VM. Pass this as user_data to the provider-specific VM resource."
  value       = local.cloud_init
}

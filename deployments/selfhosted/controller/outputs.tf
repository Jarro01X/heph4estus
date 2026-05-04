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

output "credential_scope_version" {
  description = "Credential scoping contract version for controller-generated credentials."
  value       = local.credential_scope_version
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
  description = "Backward-compatible MinIO operator access key."
  value       = random_id.minio_operator_access_key.hex
  sensitive   = true
}

output "s3_secret_key" {
  description = "Backward-compatible MinIO operator secret key."
  value       = random_password.minio_operator_secret_key.result
  sensitive   = true
}

output "s3_operator_access_key" {
  description = "MinIO operator access key."
  value       = random_id.minio_operator_access_key.hex
  sensitive   = true
}

output "s3_operator_secret_key" {
  description = "MinIO operator secret key."
  value       = random_password.minio_operator_secret_key.result
  sensitive   = true
}

output "s3_worker_access_key" {
  description = "MinIO worker access key."
  value       = random_id.minio_worker_access_key.hex
  sensitive   = true
}

output "s3_worker_secret_key" {
  description = "MinIO worker secret key, intended for worker cloud-init only."
  value       = random_password.minio_worker_secret_key.result
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

output "registry_username" {
  description = "Backward-compatible Docker registry publisher username."
  value       = local.registry_publisher_user
}

output "registry_password" {
  description = "Backward-compatible Docker registry publisher password."
  value       = random_password.registry_publisher_password.result
  sensitive   = true
}

output "registry_publisher_username" {
  description = "Docker registry publisher username."
  value       = local.registry_publisher_user
}

output "registry_publisher_password" {
  description = "Docker registry publisher password."
  value       = random_password.registry_publisher_password.result
  sensitive   = true
}

output "registry_worker_username" {
  description = "Docker registry worker username, intended for worker image pulls."
  value       = local.registry_worker_user
}

output "registry_worker_password" {
  description = "Docker registry worker password, intended for worker cloud-init image pulls only."
  value       = random_password.registry_worker_password.result
  sensitive   = true
}

output "nats_user" {
  description = "Backward-compatible NATS operator authentication username."
  value       = local.nats_operator_user
}

output "nats_password" {
  description = "Backward-compatible NATS operator authentication password."
  value       = random_password.nats_password.result
  sensitive   = true
}

output "nats_operator_user" {
  description = "NATS operator authentication username."
  value       = local.nats_operator_user
}

output "nats_operator_password" {
  description = "NATS operator authentication password."
  value       = random_password.nats_password.result
  sensitive   = true
}

output "nats_worker_user" {
  description = "NATS worker authentication username."
  value       = local.nats_worker_user
}

output "nats_worker_password" {
  description = "NATS worker authentication password, intended for worker cloud-init only."
  value       = random_password.nats_worker_password.result
  sensitive   = true
}

output "cloud_init" {
  description = "Rendered cloud-init content for the controller VM. Pass this as user_data to the provider-specific VM resource."
  value       = local.cloud_init
}

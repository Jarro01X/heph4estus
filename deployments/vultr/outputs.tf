# Output names map onto the selfhosted env family so the Go deploy UX can
# wire these directly into the operator's environment.  Sensitive outputs
# are marked so Terraform does not display them in plan/apply output.
#
# sqs_queue_url is intentionally used for the NATS subject name to maintain
# compatibility with the existing scan launch code which reads
# outputs["sqs_queue_url"].

output "tool_name" {
  description = "Tool name (passed through for lifecycle mismatch detection)."
  value       = var.tool_name
}

output "cloud" {
  description = "Cloud provider identifier."
  value       = "vultr"
}

output "controller_security_mode" {
  description = "Controller service security mode."
  value       = module.controller.controller_security_mode
}

output "credential_scope_version" {
  description = "Credential scoping contract version for controller-generated credentials."
  value       = module.controller.credential_scope_version
}

output "nats_tls_enabled" {
  description = "Whether the NATS client listener is configured for TLS."
  value       = module.controller.nats_tls_enabled
}

output "nats_mtls_enabled" {
  description = "Whether the NATS client listener requires client certificates."
  value       = module.controller.nats_mtls_enabled
}

output "nats_auth_enabled" {
  description = "Whether the NATS client listener requires authentication."
  value       = module.controller.nats_auth_enabled
}

output "minio_tls_enabled" {
  description = "Whether the MinIO S3 API is configured for TLS."
  value       = module.controller.minio_tls_enabled
}

output "minio_auth_enabled" {
  description = "Whether MinIO requires credentials."
  value       = module.controller.minio_auth_enabled
}

output "registry_tls_enabled" {
  description = "Whether the controller registry is configured for TLS."
  value       = module.controller.registry_tls_enabled
}

output "registry_auth_enabled" {
  description = "Whether the controller registry requires authentication."
  value       = module.controller.registry_auth_enabled
}

output "controller_ca_pem" {
  description = "PEM-encoded controller CA certificate used to trust TLS endpoints."
  value       = module.controller.controller_ca_pem
}

output "controller_ca_fingerprint_sha256" {
  description = "SHA-256 fingerprint of the controller CA PEM."
  value       = module.controller.controller_ca_fingerprint_sha256
}

output "controller_cert_not_after" {
  description = "RFC3339 expiration timestamp for the controller server certificate."
  value       = module.controller.controller_cert_not_after
}

output "controller_cert_generation" {
  description = "Controller certificate generation marker."
  value       = module.controller.controller_cert_generation
}

output "controller_cert_rotated_at" {
  description = "RFC3339 timestamp for the last controller certificate rotation."
  value       = module.controller.controller_cert_rotated_at
}

output "controller_host" {
  description = "Stable DNS name workers map to the controller private IP."
  value       = module.controller.controller_host
}

output "nats_url" {
  description = "NATS client URL for the operator CLI (includes operator auth credentials)."
  value       = "${module.controller.nats_tls_enabled ? "tls" : "nats"}://${local.nats_operator_user}:${local.nats_operator_password}@${vultr_instance.controller.main_ip}:4222"
  sensitive   = true
}

output "nats_user" {
  description = "Backward-compatible NATS operator authentication username."
  value       = local.nats_operator_user
}

output "nats_password" {
  description = "Backward-compatible NATS operator authentication password."
  value       = local.nats_operator_password
  sensitive   = true
}

output "nats_operator_user" {
  description = "NATS operator authentication username."
  value       = local.nats_operator_user
}

output "nats_operator_password" {
  description = "NATS operator authentication password."
  value       = local.nats_operator_password
  sensitive   = true
}

output "nats_credential_generation" {
  description = "NATS credential generation marker."
  value       = var.nats_credential_generation
}

output "nats_credential_rotated_at" {
  description = "RFC3339 timestamp for the last NATS credential rotation."
  value       = var.nats_credential_rotated_at
}

output "nats_stream" {
  description = "NATS JetStream stream name."
  value       = module.controller.nats_stream
}

output "nats_operator_client_cert_pem" {
  description = "PEM-encoded NATS mTLS client certificate for operator connections."
  value       = module.controller.nats_operator_client_cert_pem
  sensitive   = true
}

output "nats_operator_client_key_pem" {
  description = "PEM-encoded NATS mTLS client private key for operator connections."
  value       = module.controller.nats_operator_client_key_pem
  sensitive   = true
}

output "s3_endpoint" {
  description = "MinIO S3-compatible endpoint URL."
  value       = "${module.controller.minio_tls_enabled ? "https" : "http"}://${vultr_instance.controller.main_ip}:9000"
}

output "s3_region" {
  description = "S3 region (MinIO ignores this but clients require it)."
  value       = "us-east-1"
}

output "s3_access_key" {
  description = "Backward-compatible MinIO operator access key."
  value       = local.minio_operator_access_key
  sensitive   = true
}

output "s3_secret_key" {
  description = "Backward-compatible MinIO operator secret key."
  value       = local.minio_operator_secret_key
  sensitive   = true
}

output "s3_operator_access_key" {
  description = "MinIO operator access key."
  value       = local.minio_operator_access_key
  sensitive   = true
}

output "s3_operator_secret_key" {
  description = "MinIO operator secret key."
  value       = local.minio_operator_secret_key
  sensitive   = true
}

output "minio_credential_generation" {
  description = "MinIO credential generation marker."
  value       = var.minio_credential_generation
}

output "minio_credential_rotated_at" {
  description = "RFC3339 timestamp for the last MinIO credential rotation."
  value       = var.minio_credential_rotated_at
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
  value       = "${module.controller.registry_tls_enabled ? "https" : "http"}://${vultr_instance.controller.main_ip}:5000"
}

output "registry_username" {
  description = "Backward-compatible Docker registry publisher username."
  value       = local.registry_publisher_username
}

output "registry_password" {
  description = "Backward-compatible Docker registry publisher password."
  value       = local.registry_publisher_password
  sensitive   = true
}

output "registry_publisher_username" {
  description = "Docker registry publisher username."
  value       = local.registry_publisher_username
}

output "registry_publisher_password" {
  description = "Docker registry publisher password."
  value       = local.registry_publisher_password
  sensitive   = true
}

output "registry_credential_generation" {
  description = "Docker registry credential generation marker."
  value       = var.registry_credential_generation
}

output "registry_credential_rotated_at" {
  description = "RFC3339 timestamp for the last Docker registry credential rotation."
  value       = var.registry_credential_rotated_at
}

output "docker_image" {
  description = "Worker Docker image path relative to the controller registry."
  value       = var.docker_image
}

output "sqs_queue_url" {
  description = "NATS subject name (named sqs_queue_url for scan launch compatibility)."
  value       = module.controller.nats_stream
}

output "worker_count" {
  description = "Number of worker VMs in the fleet."
  value       = var.worker_count
}

output "controller_ip" {
  description = "Controller public IPv4 address."
  value       = vultr_instance.controller.main_ip
}

output "controller_ipv6" {
  description = "Controller public IPv6 address."
  value       = vultr_instance.controller.v6_main_ip
}

output "worker_ips" {
  description = "List of worker public IPv4 addresses."
  value       = vultr_instance.worker[*].main_ip
}

output "worker_ipv6s" {
  description = "List of worker public IPv6 addresses."
  value       = vultr_instance.worker[*].v6_main_ip
}

output "worker_private_ips" {
  description = "List of worker private IPs on the fleet network."
  value       = vultr_instance.worker[*].internal_ip
}

output "worker_hosts" {
  description = "Comma-separated worker IPs for SELFHOSTED_WORKER_HOSTS compatibility."
  value       = join(",", vultr_instance.worker[*].main_ip)
}

output "ssh_key_name" {
  description = "SSH key name used for VM access."
  value       = var.ssh_key_name
}

output "generation_id" {
  description = "Fleet generation ID for ownership tracking."
  value       = local.generation_id
}

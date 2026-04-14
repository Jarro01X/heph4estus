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
  value       = "nats://${var.controller_ip}:${var.nats_port}"
}

output "nats_stream" {
  description = "NATS JetStream stream name."
  value       = var.nats_stream_name
}

output "s3_endpoint" {
  description = "MinIO S3-compatible endpoint URL."
  value       = "http://${var.controller_ip}:${var.minio_port}"
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
  value       = "${var.controller_ip}:${var.registry_port}"
}

output "cloud_init" {
  description = "Rendered cloud-init content for the controller VM. Pass this as user_data to the provider-specific VM resource."
  value       = local.cloud_init
}

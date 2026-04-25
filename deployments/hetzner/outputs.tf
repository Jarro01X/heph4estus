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
  value       = "hetzner"
}

output "nats_url" {
  description = "NATS client URL for workers and the operator CLI (includes auth credentials)."
  value       = "nats://${module.controller.nats_user}:${module.controller.nats_password}@${hcloud_server.controller.ipv4_address}:4222"
  sensitive   = true
}

output "nats_user" {
  description = "NATS authentication username."
  value       = module.controller.nats_user
}

output "nats_password" {
  description = "NATS authentication password."
  value       = module.controller.nats_password
  sensitive   = true
}

output "nats_stream" {
  description = "NATS JetStream stream name."
  value       = module.controller.nats_stream
}

output "s3_endpoint" {
  description = "MinIO S3-compatible endpoint URL."
  value       = "http://${hcloud_server.controller.ipv4_address}:9000"
}

output "s3_region" {
  description = "S3 region (MinIO ignores this but clients require it)."
  value       = "us-east-1"
}

output "s3_access_key" {
  description = "MinIO root access key."
  value       = module.controller.s3_access_key
  sensitive   = true
}

output "s3_secret_key" {
  description = "MinIO root secret key."
  value       = module.controller.s3_secret_key
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
  value       = "${hcloud_server.controller.ipv4_address}:5000"
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
  value       = hcloud_server.controller.ipv4_address
}

output "controller_ipv6" {
  description = "Controller public IPv6 address."
  value       = hcloud_server.controller.ipv6_address
}

output "worker_ips" {
  description = "List of worker public IPv4 addresses."
  value       = hcloud_server.worker[*].ipv4_address
}

output "worker_ipv6s" {
  description = "List of worker public IPv6 addresses."
  value       = hcloud_server.worker[*].ipv6_address
}

output "worker_private_ips" {
  description = "List of worker private IPs on the fleet network."
  value       = [for i in range(var.worker_count) : "10.0.1.${i + 10}"]
}

output "worker_hosts" {
  description = "Comma-separated worker IPs for SELFHOSTED_WORKER_HOSTS compatibility."
  value       = join(",", hcloud_server.worker[*].ipv4_address)
}

output "ssh_key_name" {
  description = "SSH key name used for VM access."
  value       = var.ssh_key_name
}

output "generation_id" {
  description = "Fleet generation ID for ownership tracking."
  value       = local.generation_id
}

output "vpc_id" {
  description = "ID of the VPC"
  value       = module.networking.vpc_id
}

output "vpc_ipv6_cidr_block" {
  description = "Generated IPv6 CIDR block for the VPC when IPv6 is enabled"
  value       = module.networking.vpc_ipv6_cidr_block
}

output "sqs_queue_url" {
  description = "URL of the SQS queue"
  value       = module.messaging.queue_url
}

output "ecr_repo_url" {
  description = "URL of the ECR repository"
  value       = module.compute.ecr_repository_url
}

output "s3_bucket_name" {
  description = "Name of the S3 bucket for results"
  value       = module.storage.bucket_id
}

output "ecs_cluster_name" {
  description = "Name of the ECS cluster"
  value       = module.compute.ecs_cluster_name
}

output "task_definition_arn" {
  description = "ARN of the ECS task definition"
  value       = module.compute.ecs_task_definition_arn
}

output "security_group_id" {
  description = "ID of the ECS security group"
  value       = module.networking.ecs_security_group_id
}

output "subnet_ids" {
  description = "Private subnet IDs for ECS tasks"
  value       = join(" ", module.networking.private_subnet_ids)
}

output "private_subnet_ipv6_cidr_blocks" {
  description = "IPv6 CIDR blocks assigned to private subnets when IPv6 is enabled"
  value       = module.networking.private_subnet_ipv6_cidr_blocks
}

output "public_subnet_ipv6_cidr_blocks" {
  description = "IPv6 CIDR blocks assigned to public subnets when IPv6 is enabled"
  value       = module.networking.public_subnet_ipv6_cidr_blocks
}

output "nat_gateway_public_ips" {
  description = "Public IPv4 addresses assigned to NAT gateways"
  value       = module.networking.nat_gateway_public_ips
}

output "egress_only_internet_gateway_id" {
  description = "ID of the egress-only internet gateway when IPv6 is enabled"
  value       = module.networking.egress_only_internet_gateway_id
}

output "instance_profile_arn" {
  description = "ARN of the IAM instance profile for spot workers"
  value       = module.spot.instance_profile_arn
}

output "ami_id" {
  description = "AMI ID for spot instances"
  value       = module.spot.ami_id
}

output "tool_name" {
  description = "Name of the tool this infrastructure was deployed for"
  value       = var.tool_name
}

output "vpc_id" {
  description = "ID of the VPC"
  value       = module.networking.vpc_id
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

output "instance_profile_arn" {
  description = "ARN of the IAM instance profile for spot workers"
  value       = module.spot.instance_profile_arn
}

output "ami_id" {
  description = "AMI ID for spot instances"
  value       = module.spot.ami_id
}

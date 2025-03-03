output "vpc_id" {
  description = "ID of the VPC"
  value       = module.networking.vpc_id
}

output "state_machine_arn" {
  description = "ARN of the Step Functions state machine"
  value       = module.orchestration.state_machine_arn
}

output "sqs_queue_url" {
  description = "URL of the SQS queue"
  value       = module.messaging.queue_url
}

output "ecr_repository_url" {
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
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

output "deployment_command" {
  description = "Command to deploy a new image to ECR"
  value       = <<-EOT
    # Build and push the container image
    aws ecr get-login-password --region ${var.aws_region} | docker login --username AWS --password-stdin $(aws sts get-caller-identity --query Account --output text).dkr.ecr.${var.aws_region}.amazonaws.com
    docker build -t ${module.compute.ecr_repository_url}:latest .
    docker push ${module.compute.ecr_repository_url}:latest
  EOT
}

output "run_command" {
  description = "Command to run the producer application"
  value       = <<-EOT
    # Set the STATE_MACHINE_ARN environment variable
    export STATE_MACHINE_ARN="${module.orchestration.state_machine_arn}"
    
    # Run the producer application with a targets file
    go run cmd/producer/main.go -file=targets.txt
  EOT
}
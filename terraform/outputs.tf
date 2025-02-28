output "step_function_arn" {
  description = "ARN of the Step Functions state machine"
  value       = aws_sfn_state_machine.nmap_scanner.arn
}

output "sqs_queue_url" {
  description = "URL of the SQS queue"
  value       = aws_sqs_queue.scan_tasks.url
}

output "ecr_repository_url" {
  description = "URL of the ECR repository"
  value       = aws_ecr_repository.scanner.repository_url
}

output "s3_bucket_name" {
  description = "Name of the S3 bucket for results"
  value       = aws_s3_bucket.results.id
}
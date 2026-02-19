output "queue_url" {
  description = "URL of the SQS queue for scan tasks"
  value       = aws_sqs_queue.tasks.url
}

output "queue_arn" {
  description = "ARN of the SQS queue for scan tasks"
  value       = aws_sqs_queue.tasks.arn
}

output "dlq_url" {
  description = "URL of the dead-letter queue"
  value       = aws_sqs_queue.dlq.url
}

output "dlq_arn" {
  description = "ARN of the dead-letter queue"
  value       = aws_sqs_queue.dlq.arn
}
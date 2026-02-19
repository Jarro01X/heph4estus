# Main SQS queue for scan tasks
resource "aws_sqs_queue" "tasks" {
  name                       = "${var.name_prefix}-scan-tasks"
  visibility_timeout_seconds = 900  # 15 minutes
  message_retention_seconds  = 86400  # 1 day
  
  redrive_policy = jsonencode({
    deadLetterTargetArn = aws_sqs_queue.dlq.arn
    maxReceiveCount     = 3
  })

  tags = {
    Name        = "${var.name_prefix}-scan-tasks"
    Environment = var.environment
    Terraform   = "true"
  }
}

# Dead letter queue for failed tasks
resource "aws_sqs_queue" "dlq" {
  name                      = "${var.name_prefix}-scan-tasks-dlq"
  message_retention_seconds = 1209600  # 14 days

  tags = {
    Name        = "${var.name_prefix}-scan-tasks-dlq"
    Environment = var.environment
    Terraform   = "true"
  }
}
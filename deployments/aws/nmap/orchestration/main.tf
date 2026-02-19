# Step Functions state machine
resource "aws_sfn_state_machine" "nmap_scanner" {
  name     = "${var.name_prefix}-workflow"
  role_arn = var.step_functions_role_arn

  definition = jsonencode({
    Comment = "Nmap scanning workflow"
    StartAt = "ProcessTargets"
    States = {
      ProcessTargets = {
        Type = "Map"
        ItemsPath = "$.targets"
        MaxConcurrency = var.max_concurrency
        Iterator = {
          StartAt = "EnqueueTask"
          States = {
            EnqueueTask = {
              Type = "Task"
              Resource = "arn:aws:states:::sqs:sendMessage"
              Parameters = {
                QueueUrl = var.sqs_queue_url
                MessageBody = {
                  "target.$" = "$.target"
                  "options.$" = "$.options"
                }
              }
              Retry = [
                {
                  ErrorEquals = ["States.ALL"]
                  IntervalSeconds = 2
                  MaxAttempts = 3
                  BackoffRate = 2
                }
              ]
              Next = "StartECSTask"
            }
            StartECSTask = {
              Type = "Task"
              Resource = "arn:aws:states:::ecs:runTask.sync"
              Parameters = {
                LaunchType = "FARGATE"
                Cluster = var.ecs_cluster_arn
                TaskDefinition = var.ecs_task_definition_arn
                NetworkConfiguration = {
                  AwsvpcConfiguration = {
                    Subnets = var.private_subnet_ids
                    SecurityGroups = [var.ecs_security_group_id]
                  }
                }
              }
              # Add timeout for ECS task - 8 minutes
              TimeoutSeconds = var.task_timeout_seconds
              Retry = [
                {
                  ErrorEquals = ["States.ALL"]
                  IntervalSeconds = 3
                  MaxAttempts = 2
                  BackoffRate = 1.5
                }
              ]
              End = true
            }
          }
        }
        End = true
      }
    }
  })

  logging_configuration {
    log_destination        = "${aws_cloudwatch_log_group.step_functions.arn}:*"
    include_execution_data = true
    level                  = "ALL"
  }

  tags = {
    Name        = "${var.name_prefix}-workflow"
    Environment = var.environment
    Terraform   = "true"
  }
}

# CloudWatch log group for Step Functions
resource "aws_cloudwatch_log_group" "step_functions" {
  name              = "/aws/states/${var.name_prefix}-workflow"
  retention_in_days = var.log_retention_days

  tags = {
    Name        = "${var.name_prefix}-sfn-logs"
    Environment = var.environment
    Terraform   = "true"
  }
}
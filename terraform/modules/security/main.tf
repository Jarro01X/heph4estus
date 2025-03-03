data "aws_caller_identity" "current" {}
data "aws_region" "current" {}

# ===== Step Functions Role =====
resource "aws_iam_role" "step_functions" {
  name = "${var.name_prefix}-step-functions-role"

  assume_role_policy = jsonencode({
    Version = "2012-10-17"
    Statement = [
      {
        Effect = "Allow"
        Principal = {
          Service = "states.amazonaws.com"
        }
        Action = "sts:AssumeRole"
      }
    ]
  })

  tags = {
    Name        = "${var.name_prefix}-step-functions-role"
    Environment = var.environment
    Terraform   = "true"
  }
}

# Custom policy for Step Functions logging and X-Ray
resource "aws_iam_policy" "step_functions_logs" {
  name        = "${var.name_prefix}-step-functions-logs-policy"
  description = "Policy for Step Functions logs and X-Ray"
  
  policy = jsonencode({
    Version = "2012-10-17"
    Statement = [
      {
        Effect = "Allow"
        Action = [
          "logs:CreateLogDelivery",
          "logs:GetLogDelivery",
          "logs:UpdateLogDelivery",
          "logs:DeleteLogDelivery",
          "logs:ListLogDeliveries",
          "logs:PutResourcePolicy",
          "logs:DescribeResourcePolicies",
          "logs:DescribeLogGroups",
          "xray:PutTraceSegments",
          "xray:PutTelemetryRecords",
          "xray:GetSamplingRules",
          "xray:GetSamplingTargets",
          "xray:GetSamplingStatisticSummaries"
        ]
        Resource = "*"
      }
    ]
  })
}

# Custom policy for Step Functions to interact with other services
resource "aws_iam_policy" "step_functions_services" {
  name        = "${var.name_prefix}-step-functions-services-policy"
  description = "Policy for Step Functions to interact with other AWS services"
  
  policy = jsonencode({
    Version = "2012-10-17"
    Statement = [
      {
        Effect = "Allow"
        Action = [
          "sqs:SendMessage",
          "ecs:RunTask",
          "ecs:StopTask",
          "ecs:DescribeTasks",
          "iam:PassRole",
          "events:PutTargets",
          "events:PutRule",
          "events:DescribeRule"
        ]
        Resource = "*"
      }
    ]
  })
}

# Attach policies to Step Functions role
resource "aws_iam_role_policy_attachment" "step_functions_logs" {
  role       = aws_iam_role.step_functions.name
  policy_arn = aws_iam_policy.step_functions_logs.arn
}

resource "aws_iam_role_policy_attachment" "step_functions_services" {
  role       = aws_iam_role.step_functions.name
  policy_arn = aws_iam_policy.step_functions_services.arn
}

# ===== ECS Execution Role =====
resource "aws_iam_role" "ecs_execution" {
  name = "${var.name_prefix}-ecs-execution-role"

  assume_role_policy = jsonencode({
    Version = "2012-10-17"
    Statement = [
      {
        Action = "sts:AssumeRole"
        Effect = "Allow"
        Principal = {
          Service = "ecs-tasks.amazonaws.com"
        }
      }
    ]
  })

  tags = {
    Name        = "${var.name_prefix}-ecs-execution-role"
    Environment = var.environment
    Terraform   = "true"
  }
}

# Attach the AWS managed policy for ECS task execution
resource "aws_iam_role_policy_attachment" "ecs_execution_policy" {
  role       = aws_iam_role.ecs_execution.name
  policy_arn = "arn:aws:iam::aws:policy/service-role/AmazonECSTaskExecutionRolePolicy"
}

# Custom policy for ECR and CloudWatch Logs
resource "aws_iam_policy" "ecs_execution_custom" {
  name        = "${var.name_prefix}-ecs-execution-custom-policy"
  description = "Custom policy for ECS execution role"
  
  policy = jsonencode({
    Version = "2012-10-17"
    Statement = [
      {
        Effect = "Allow"
        Action = [
          "ecr:GetAuthorizationToken",
          "ecr:BatchCheckLayerAvailability",
          "ecr:GetDownloadUrlForLayer",
          "ecr:BatchGetImage",
          "logs:CreateLogStream",
          "logs:PutLogEvents"
        ]
        Resource = "*"
      },
      {
        Effect = "Allow"
        Action = [
          "logs:CreateLogGroup"
        ]
        Resource = "arn:aws:logs:${data.aws_region.current.name}:${data.aws_caller_identity.current.account_id}:*"
      }
    ]
  })
}

resource "aws_iam_role_policy_attachment" "ecs_execution_custom" {
  role       = aws_iam_role.ecs_execution.name
  policy_arn = aws_iam_policy.ecs_execution_custom.arn
}

# ===== ECS Task Role =====
resource "aws_iam_role" "ecs_task" {
  name = "${var.name_prefix}-ecs-task-role"

  assume_role_policy = jsonencode({
    Version = "2012-10-17"
    Statement = [{
      Action = "sts:AssumeRole"
      Effect = "Allow"
      Principal = {
        Service = "ecs-tasks.amazonaws.com"
      }
    }]
  })

  tags = {
    Name        = "${var.name_prefix}-ecs-task-role"
    Environment = var.environment
    Terraform   = "true"
  }
}

# Custom policy for ECS task to access SQS and S3
resource "aws_iam_policy" "ecs_task_custom" {
  name        = "${var.name_prefix}-ecs-task-custom-policy"
  description = "Custom policy for ECS task role"
  
  policy = jsonencode({
    Version = "2012-10-17"
    Statement = [
      {
        Effect = "Allow"
        Action = [
          "sqs:ReceiveMessage",
          "sqs:DeleteMessage",
          "sqs:GetQueueAttributes"
        ]
        Resource = var.sqs_queue_arn
      },
      {
        Effect = "Allow"
        Action = [
          "s3:PutObject"
        ]
        Resource = "${var.s3_bucket_arn}/*"
      }
    ]
  })
}

resource "aws_iam_role_policy_attachment" "ecs_task_custom" {
  role       = aws_iam_role.ecs_task.name
  policy_arn = aws_iam_policy.ecs_task_custom.arn
}
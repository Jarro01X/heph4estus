locals {
  base_env = [
    {
      name  = "QUEUE_URL"
      value = var.sqs_queue_url
    },
    {
      name  = "S3_BUCKET"
      value = var.s3_bucket_id
    },
    {
      name  = "TOOL_NAME"
      value = var.tool_name
    },
    {
      name  = "JITTER_MAX_SECONDS"
      value = tostring(var.jitter_max_seconds)
    },
  ]
  extra_env = [for k, v in var.container_env_vars : { name = k, value = v }]
  all_env   = concat(local.base_env, local.extra_env)
}

# CloudWatch log group for ECS tasks
resource "aws_cloudwatch_log_group" "worker_logs" {
  name              = "/ecs/${var.name_prefix}-${var.tool_name}"
  retention_in_days = var.log_retention_days

  tags = {
    Environment = var.environment
    Application = "${var.tool_name}-worker"
    Terraform   = "true"
  }
}

# ECR repository for container images
resource "aws_ecr_repository" "worker" {
  name                 = "${var.name_prefix}-${var.tool_name}"
  image_tag_mutability = "MUTABLE"
  force_delete         = true

  image_scanning_configuration {
    scan_on_push = true
  }

  tags = {
    Name        = "${var.name_prefix}-${var.tool_name}"
    Environment = var.environment
    Terraform   = "true"
  }
}

# Repository lifecycle policy to limit image versions
resource "aws_ecr_lifecycle_policy" "worker" {
  repository = aws_ecr_repository.worker.name

  policy = jsonencode({
    rules = [
      {
        rulePriority = 1
        description  = "Keep only the 10 most recent images"
        selection = {
          tagStatus   = "any"
          countType   = "imageCountMoreThan"
          countNumber = 10
        }
        action = {
          type = "expire"
        }
      }
    ]
  })
}

# ECS cluster
resource "aws_ecs_cluster" "worker" {
  name = "${var.name_prefix}-cluster"

  setting {
    name  = "containerInsights"
    value = "enabled"
  }

  tags = {
    Name        = "${var.name_prefix}-cluster"
    Environment = var.environment
    Terraform   = "true"
  }
}

# ECS task definition
resource "aws_ecs_task_definition" "worker" {
  family                   = "${var.name_prefix}-${var.tool_name}"
  requires_compatibilities = ["FARGATE"]
  network_mode             = "awsvpc"
  cpu                      = var.task_cpu
  memory                   = var.task_memory
  execution_role_arn       = var.ecs_execution_role_arn
  task_role_arn            = var.ecs_task_role_arn

  container_definitions = jsonencode([
    {
      name        = "${var.tool_name}-worker"
      image       = "${aws_ecr_repository.worker.repository_url}:latest"
      environment = local.all_env
      logConfiguration = {
        logDriver = "awslogs"
        options = {
          awslogs-group         = aws_cloudwatch_log_group.worker_logs.name
          awslogs-region        = var.aws_region
          awslogs-stream-prefix = var.tool_name
        }
      }
      essential = true
    }
  ])

  tags = {
    Name        = "${var.name_prefix}-${var.tool_name}-task-definition"
    Environment = var.environment
    Terraform   = "true"
  }
}

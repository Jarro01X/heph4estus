# CloudWatch log group for ECS tasks
resource "aws_cloudwatch_log_group" "scanner_logs" {
  name              = "/ecs/${var.name_prefix}-scanner"
  retention_in_days = var.log_retention_days
  
  tags = {
    Environment = var.environment
    Application = "nmap-scanner"
    Terraform   = "true"
  }
}

# ECR repository for container images
resource "aws_ecr_repository" "scanner" {
  name                 = "${var.name_prefix}-scanner"
  image_tag_mutability = "MUTABLE"
  force_delete         = true

  image_scanning_configuration {
    scan_on_push = true
  }

  tags = {
    Name        = "${var.name_prefix}-scanner"
    Environment = var.environment
    Terraform   = "true"
  }
}

# Repository lifecycle policy to limit image versions
resource "aws_ecr_lifecycle_policy" "scanner" {
  repository = aws_ecr_repository.scanner.name

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
resource "aws_ecs_cluster" "scanner" {
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
resource "aws_ecs_task_definition" "scanner" {
  family                   = "${var.name_prefix}-scanner"
  requires_compatibilities = ["FARGATE"]
  network_mode             = "awsvpc"
  cpu                      = var.task_cpu
  memory                   = var.task_memory
  execution_role_arn       = var.ecs_execution_role_arn
  task_role_arn            = var.ecs_task_role_arn

  container_definitions = jsonencode([
    {
      name  = "nmap-scanner"
      image = "${aws_ecr_repository.scanner.repository_url}:latest"
      environment = [
        {
          name  = "QUEUE_URL"
          value = var.sqs_queue_url
        },
        {
          name  = "S3_BUCKET"
          value = var.s3_bucket_id
        }
      ]
      logConfiguration = {
        logDriver = "awslogs"
        options = {
          awslogs-group         = aws_cloudwatch_log_group.scanner_logs.name
          awslogs-region        = var.aws_region
          awslogs-stream-prefix = "scanner"
        }
      }
      essential = true
    }
  ])

  tags = {
    Name        = "${var.name_prefix}-task-definition"
    Environment = var.environment
    Terraform   = "true"
  }
}
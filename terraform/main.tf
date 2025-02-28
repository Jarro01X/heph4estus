data "aws_availability_zones" "available" {
  state = "available"
}

data "aws_caller_identity" "current" {}

resource "random_string" "suffix" {
  length  = 8
  special = false
  upper   = false
}

resource "aws_cloudwatch_log_group" "scanner_logs" {
  name              = "/ecs/${var.environment}-scanner"
  retention_in_days = 30
  
  tags = {
    Environment = var.environment
    Application = "nmap-scanner"
  }
}

# Set up the VPC and networking
resource "aws_vpc" "main" {
  cidr_block           = var.vpc_cidr
  enable_dns_hostnames = true
  enable_dns_support   = true

  tags = {
    Name        = "${var.environment}-vpc"
    Environment = var.environment
  }
}

resource "aws_subnet" "public" {
  count             = 2
  vpc_id            = aws_vpc.main.id
  cidr_block        = cidrsubnet(var.vpc_cidr, 8, count.index + 2)  # +2 to avoid overlap with private subnets
  availability_zone = data.aws_availability_zones.available.names[count.index]
  map_public_ip_on_launch = true

  tags = {
    Name        = "${var.environment}-public-${count.index + 1}"
    Environment = var.environment
  }
}

# Create Internet Gateway
resource "aws_internet_gateway" "main" {
  vpc_id = aws_vpc.main.id

  tags = {
    Name        = "${var.environment}-igw"
    Environment = var.environment
  }
}

# Create NAT Gateway
resource "aws_eip" "nat" {
  tags = {
    Name        = "${var.environment}-nat-eip"
    Environment = var.environment
  }
}

resource "aws_nat_gateway" "main" {
  allocation_id = aws_eip.nat.id
  subnet_id     = aws_subnet.public[0].id  # Place in first public subnet

  tags = {
    Name        = "${var.environment}-nat"
    Environment = var.environment
  }

  depends_on = [aws_internet_gateway.main]
}

# Route table for public subnets
resource "aws_route_table" "public" {
  vpc_id = aws_vpc.main.id

  route {
    cidr_block = "0.0.0.0/0"
    gateway_id = aws_internet_gateway.main.id
  }

  tags = {
    Name        = "${var.environment}-public"
    Environment = var.environment
  }
}

# Route table for private subnets
resource "aws_route_table" "private" {
  vpc_id = aws_vpc.main.id

  route {
    cidr_block = "0.0.0.0/0"
    nat_gateway_id = aws_nat_gateway.main.id
  }

  tags = {
    Name        = "${var.environment}-private"
    Environment = var.environment
  }
}

# Associate public subnets with public route table
resource "aws_route_table_association" "public" {
  count          = 2
  subnet_id      = aws_subnet.public[count.index].id
  route_table_id = aws_route_table.public.id
}

# Associate private subnets with private route table
resource "aws_route_table_association" "private" {
  count          = 2
  subnet_id      = aws_subnet.private[count.index].id
  route_table_id = aws_route_table.private.id
}

# Create private subnets for our ECS tasks
resource "aws_subnet" "private" {
  count             = 2
  vpc_id            = aws_vpc.main.id
  cidr_block        = cidrsubnet(var.vpc_cidr, 8, count.index)
  availability_zone = data.aws_availability_zones.available.names[count.index]

  tags = {
    Name        = "${var.environment}-private-${count.index + 1}"
    Environment = var.environment
  }
}

# Set up SQS queues for task distribution
resource "aws_sqs_queue" "scan_tasks" {
  name                      = "${var.environment}-scan-tasks"
  visibility_timeout_seconds = 900  # 15 minutes
  message_retention_seconds = 86400  # 1 day
  
  redrive_policy = jsonencode({
    deadLetterTargetArn = aws_sqs_queue.scan_dlq.arn
    maxReceiveCount     = 3
  })
}

# Dead letter queue for failed tasks
resource "aws_sqs_queue" "scan_dlq" {
  name                      = "${var.environment}-scan-tasks-dlq"
  message_retention_seconds = 1209600  # 14 days
}

# ECR repository for our container images
resource "aws_ecr_repository" "scanner" {
  name                 = "${var.environment}-nmap-scanner"
  image_tag_mutability = "MUTABLE"
  force_delete         = true

  image_scanning_configuration {
    scan_on_push = true
  }
}

# S3 bucket for scan results
resource "aws_s3_bucket" "results" {
  bucket        = "nmap-scan-results-${random_string.suffix.result}"
  force_destroy = false

  tags = {
    Environment = var.environment
  }
}

# Block all public access to the S3 bucket
resource "aws_s3_bucket_public_access_block" "results" {
  bucket = aws_s3_bucket.results.id

  block_public_acls       = true
  block_public_policy     = true
  ignore_public_acls      = true
  restrict_public_buckets = true
}

# ECS cluster for running our scanner tasks
resource "aws_ecs_cluster" "scanner" {
  name = "${var.environment}-scanner-cluster"

  setting {
    name  = "containerInsights"
    value = "enabled"
  }
}

resource "aws_ecs_task_definition" "scanner" {
  family                   = "${var.environment}-nmap-scanner"
  requires_compatibilities = ["FARGATE"]
  network_mode            = "awsvpc"
  cpu                     = 256
  memory                  = 512
  execution_role_arn      = aws_iam_role.ecs_execution.arn
  task_role_arn           = aws_iam_role.ecs_task.arn

  container_definitions = jsonencode([
    {
      name  = "nmap-scanner"
      image = "${aws_ecr_repository.scanner.repository_url}:latest"
      environment = [
        {
          name  = "QUEUE_URL"
          value = aws_sqs_queue.scan_tasks.url
        },
        {
          name  = "S3_BUCKET"
          value = aws_s3_bucket.results.id
        }
      ]
      logConfiguration = {
        logDriver = "awslogs"
        options = {
          awslogs-group         = "/ecs/${var.environment}-scanner"
          awslogs-region        = var.aws_region
          awslogs-stream-prefix = "scanner"
        }
      }
    }
  ])
}

resource "aws_security_group" "ecs_tasks" {
  name        = "${var.environment}-ecs-tasks-sg"
  description = "Security group for nmap scanner ECS tasks"
  vpc_id      = aws_vpc.main.id

  # Allow outbound internet access for nmap scanning
  egress {
    from_port   = 0
    to_port     = 0
    protocol    = "-1"
    cidr_blocks = ["0.0.0.0/0"]
  }

  # Since nmap is doing the scanning, we don't need inbound rules
  # The containers will pull tasks from SQS and push results to S3
  # Both operations are outbound connections

  tags = {
    Name        = "${var.environment}-ecs-tasks-sg"
    Environment = var.environment
    Purpose     = "nmap-scanner"
  }
}
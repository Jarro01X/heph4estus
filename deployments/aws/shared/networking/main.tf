terraform {
  required_version = ">= 1.5.0"

  required_providers {
    aws = {
      source  = "hashicorp/aws"
      version = "~> 6.0"
    }
  }
}

data "aws_availability_zones" "available" {
  state = "available"
}

locals {
  nat_gateway_count         = var.multi_nat ? var.az_count : 1
  private_route_table_count = var.multi_nat ? var.az_count : 1
}

moved {
  from = aws_eip.nat
  to   = aws_eip.nat[0]
}

moved {
  from = aws_nat_gateway.this
  to   = aws_nat_gateway.this[0]
}

moved {
  from = aws_route_table.private
  to   = aws_route_table.private[0]
}

# VPC for the application
resource "aws_vpc" "this" {
  cidr_block                       = var.vpc_cidr
  assign_generated_ipv6_cidr_block = var.enable_ipv6
  enable_dns_hostnames             = true
  enable_dns_support               = true

  tags = {
    Name        = "${var.name_prefix}-vpc"
    Environment = var.environment
    Terraform   = "true"
  }
}

# Public subnets for NAT gateway and internet-facing resources
resource "aws_subnet" "public" {
  count                           = var.az_count
  vpc_id                          = aws_vpc.this.id
  cidr_block                      = cidrsubnet(var.vpc_cidr, 8, count.index + var.az_count)
  ipv6_cidr_block                 = var.enable_ipv6 ? cidrsubnet(aws_vpc.this.ipv6_cidr_block, 8, count.index + var.az_count) : null
  availability_zone               = data.aws_availability_zones.available.names[count.index]
  map_public_ip_on_launch         = false
  assign_ipv6_address_on_creation = var.enable_ipv6

  tags = {
    Name        = "${var.name_prefix}-public-${count.index + 1}"
    Environment = var.environment
    Terraform   = "true"
  }
}

# Private subnets for ECS tasks and internal resources
resource "aws_subnet" "private" {
  count                           = var.az_count
  vpc_id                          = aws_vpc.this.id
  cidr_block                      = cidrsubnet(var.vpc_cidr, 8, count.index)
  ipv6_cidr_block                 = var.enable_ipv6 ? cidrsubnet(aws_vpc.this.ipv6_cidr_block, 8, count.index) : null
  availability_zone               = data.aws_availability_zones.available.names[count.index]
  assign_ipv6_address_on_creation = var.enable_ipv6

  tags = {
    Name        = "${var.name_prefix}-private-${count.index + 1}"
    Environment = var.environment
    Terraform   = "true"
  }
}

# Internet Gateway
resource "aws_internet_gateway" "this" {
  vpc_id = aws_vpc.this.id

  tags = {
    Name        = "${var.name_prefix}-igw"
    Environment = var.environment
    Terraform   = "true"
  }
}

# Egress-only Internet Gateway for private IPv6 egress
resource "aws_egress_only_internet_gateway" "this" {
  count  = var.enable_ipv6 ? 1 : 0
  vpc_id = aws_vpc.this.id

  tags = {
    Name        = "${var.name_prefix}-eigw"
    Environment = var.environment
    Terraform   = "true"
  }
}

# Elastic IP for NAT Gateway
resource "aws_eip" "nat" {
  count  = local.nat_gateway_count
  domain = "vpc"

  tags = {
    Name        = var.multi_nat ? "${var.name_prefix}-nat-eip-${count.index + 1}" : "${var.name_prefix}-nat-eip"
    Environment = var.environment
    Terraform   = "true"
  }
}

# NAT Gateway
resource "aws_nat_gateway" "this" {
  count         = local.nat_gateway_count
  allocation_id = aws_eip.nat[count.index].id
  subnet_id     = aws_subnet.public[count.index].id

  tags = {
    Name        = var.multi_nat ? "${var.name_prefix}-nat-${count.index + 1}" : "${var.name_prefix}-nat"
    Environment = var.environment
    Terraform   = "true"
  }

  depends_on = [aws_internet_gateway.this]
}

# Route table for public subnets
resource "aws_route_table" "public" {
  vpc_id = aws_vpc.this.id

  route {
    cidr_block = "0.0.0.0/0"
    gateway_id = aws_internet_gateway.this.id
  }

  dynamic "route" {
    for_each = var.enable_ipv6 ? [1] : []

    content {
      ipv6_cidr_block = "::/0"
      gateway_id      = aws_internet_gateway.this.id
    }
  }

  tags = {
    Name        = "${var.name_prefix}-public-rt"
    Environment = var.environment
    Terraform   = "true"
  }
}

# Route table for private subnets
resource "aws_route_table" "private" {
  count  = local.private_route_table_count
  vpc_id = aws_vpc.this.id

  route {
    cidr_block     = "0.0.0.0/0"
    nat_gateway_id = aws_nat_gateway.this[var.multi_nat ? count.index : 0].id
  }

  dynamic "route" {
    for_each = var.enable_ipv6 ? [1] : []

    content {
      ipv6_cidr_block        = "::/0"
      egress_only_gateway_id = aws_egress_only_internet_gateway.this[0].id
    }
  }

  tags = {
    Name        = var.multi_nat ? "${var.name_prefix}-private-rt-${count.index + 1}" : "${var.name_prefix}-private-rt"
    Environment = var.environment
    Terraform   = "true"
  }
}

# Associate public subnets with public route table
resource "aws_route_table_association" "public" {
  count          = var.az_count
  subnet_id      = aws_subnet.public[count.index].id
  route_table_id = aws_route_table.public.id
}

# Associate private subnets with private route table
resource "aws_route_table_association" "private" {
  count          = var.az_count
  subnet_id      = aws_subnet.private[count.index].id
  route_table_id = aws_route_table.private[var.multi_nat ? count.index : 0].id
}

resource "aws_cloudwatch_log_group" "vpc_flow_logs" {
  name              = "/aws/vpc-flow-logs/${var.name_prefix}"
  retention_in_days = var.log_retention_days
  kms_key_id        = var.kms_key_arn

  tags = {
    Name        = "${var.name_prefix}-vpc-flow-logs"
    Environment = var.environment
    Terraform   = "true"
  }
}

resource "aws_iam_role" "vpc_flow_logs" {
  name = "${var.name_prefix}-vpc-flow-logs-role"

  assume_role_policy = jsonencode({
    Version = "2012-10-17"
    Statement = [
      {
        Effect = "Allow"
        Principal = {
          Service = "vpc-flow-logs.amazonaws.com"
        }
        Action = "sts:AssumeRole"
      }
    ]
  })

  tags = {
    Name        = "${var.name_prefix}-vpc-flow-logs-role"
    Environment = var.environment
    Terraform   = "true"
  }
}

resource "aws_iam_policy" "vpc_flow_logs" {
  name        = "${var.name_prefix}-vpc-flow-logs-policy"
  description = "Allow VPC Flow Logs to write to CloudWatch Logs"

  policy = jsonencode({
    Version = "2012-10-17"
    Statement = [
      {
        Effect = "Allow"
        Action = [
          "logs:CreateLogStream",
          "logs:PutLogEvents",
          "logs:DescribeLogStreams"
        ]
        Resource = "${aws_cloudwatch_log_group.vpc_flow_logs.arn}:*"
      },
      {
        Effect = "Allow"
        Action = [
          "logs:DescribeLogGroups"
        ]
        Resource = "*"
      }
    ]
  })
}

resource "aws_iam_role_policy_attachment" "vpc_flow_logs" {
  role       = aws_iam_role.vpc_flow_logs.name
  policy_arn = aws_iam_policy.vpc_flow_logs.arn
}

resource "aws_flow_log" "vpc" {
  iam_role_arn    = aws_iam_role.vpc_flow_logs.arn
  log_destination = aws_cloudwatch_log_group.vpc_flow_logs.arn
  traffic_type    = "ALL"
  vpc_id          = aws_vpc.this.id

  tags = {
    Name        = "${var.name_prefix}-vpc-flow-log"
    Environment = var.environment
    Terraform   = "true"
  }

  depends_on = [aws_iam_role_policy_attachment.vpc_flow_logs]
}

# Security group for ECS tasks
# Scanner workers need arbitrary target reachability by default. Operators can
# narrow scanner_egress_ipv4_cidr_blocks for constrained environments.
#trivy:ignore:AVD-AWS-0104
resource "aws_security_group" "ecs_tasks" {
  name        = "${var.name_prefix}-ecs-tasks-sg"
  description = "Security group for Nmap scanner ECS tasks"
  vpc_id      = aws_vpc.this.id

  egress {
    description = "Allow scanner egress to configured IPv4 targets"
    from_port   = 0
    to_port     = 0
    protocol    = "-1"
    cidr_blocks = var.scanner_egress_ipv4_cidr_blocks
  }

  dynamic "egress" {
    for_each = var.enable_ipv6 ? [1] : []

    content {
      description      = "Allow scanner egress to configured IPv6 targets"
      from_port        = 0
      to_port          = 0
      protocol         = "-1"
      ipv6_cidr_blocks = var.scanner_egress_ipv6_cidr_blocks
    }
  }

  tags = {
    Name        = "${var.name_prefix}-ecs-tasks-sg"
    Environment = var.environment
    Purpose     = "nmap-scanner"
    Terraform   = "true"
  }
}

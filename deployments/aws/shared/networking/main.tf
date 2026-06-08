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
  map_public_ip_on_launch         = true
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

# Security group for ECS tasks
resource "aws_security_group" "ecs_tasks" {
  name        = "${var.name_prefix}-ecs-tasks-sg"
  description = "Security group for Nmap scanner ECS tasks"
  vpc_id      = aws_vpc.this.id

  # Allow outbound internet access for Nmap scanning
  egress {
    from_port   = 0
    to_port     = 0
    protocol    = "-1"
    cidr_blocks = ["0.0.0.0/0"]
  }

  dynamic "egress" {
    for_each = var.enable_ipv6 ? [1] : []

    content {
      from_port        = 0
      to_port          = 0
      protocol         = "-1"
      ipv6_cidr_blocks = ["::/0"]
    }
  }

  tags = {
    Name        = "${var.name_prefix}-ecs-tasks-sg"
    Environment = var.environment
    Purpose     = "nmap-scanner"
    Terraform   = "true"
  }
}

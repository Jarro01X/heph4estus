data "aws_availability_zones" "available" {
  state = "available"
}

# VPC for the application
resource "aws_vpc" "this" {
  cidr_block           = var.vpc_cidr
  enable_dns_hostnames = true
  enable_dns_support   = true

  tags = {
    Name        = "${var.name_prefix}-vpc"
    Environment = var.environment
    Terraform   = "true"
  }
}

# Public subnets for NAT gateway and internet-facing resources
resource "aws_subnet" "public" {
  count                   = var.az_count
  vpc_id                  = aws_vpc.this.id
  cidr_block              = cidrsubnet(var.vpc_cidr, 8, count.index + var.az_count)
  availability_zone       = data.aws_availability_zones.available.names[count.index]
  map_public_ip_on_launch = true

  tags = {
    Name        = "${var.name_prefix}-public-${count.index + 1}"
    Environment = var.environment
    Terraform   = "true"
  }
}

# Private subnets for ECS tasks and internal resources
resource "aws_subnet" "private" {
  count             = var.az_count
  vpc_id            = aws_vpc.this.id
  cidr_block        = cidrsubnet(var.vpc_cidr, 8, count.index)
  availability_zone = data.aws_availability_zones.available.names[count.index]

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

# Elastic IP for NAT Gateway
resource "aws_eip" "nat" {
  domain = "vpc"

  tags = {
    Name        = "${var.name_prefix}-nat-eip"
    Environment = var.environment
    Terraform   = "true"
  }
}

# NAT Gateway
resource "aws_nat_gateway" "this" {
  allocation_id = aws_eip.nat.id
  subnet_id     = aws_subnet.public[0].id # Place in first public subnet

  tags = {
    Name        = "${var.name_prefix}-nat"
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

  tags = {
    Name        = "${var.name_prefix}-public-rt"
    Environment = var.environment
    Terraform   = "true"
  }
}

# Route table for private subnets
resource "aws_route_table" "private" {
  vpc_id = aws_vpc.this.id

  route {
    cidr_block     = "0.0.0.0/0"
    nat_gateway_id = aws_nat_gateway.this.id
  }

  tags = {
    Name        = "${var.name_prefix}-private-rt"
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
  route_table_id = aws_route_table.private.id
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

  tags = {
    Name        = "${var.name_prefix}-ecs-tasks-sg"
    Environment = var.environment
    Purpose     = "nmap-scanner"
    Terraform   = "true"
  }
}
output "vpc_id" {
  description = "ID of the VPC"
  value       = aws_vpc.this.id
}

output "vpc_ipv6_cidr_block" {
  description = "Generated IPv6 CIDR block for the VPC when IPv6 is enabled"
  value       = var.enable_ipv6 ? aws_vpc.this.ipv6_cidr_block : null
}

output "private_subnet_ids" {
  description = "IDs of the private subnets"
  value       = aws_subnet.private[*].id
}

output "private_subnet_ipv6_cidr_blocks" {
  description = "IPv6 CIDR blocks assigned to private subnets when IPv6 is enabled"
  value       = var.enable_ipv6 ? aws_subnet.private[*].ipv6_cidr_block : []
}

output "public_subnet_ids" {
  description = "IDs of the public subnets"
  value       = aws_subnet.public[*].id
}

output "public_subnet_ipv6_cidr_blocks" {
  description = "IPv6 CIDR blocks assigned to public subnets when IPv6 is enabled"
  value       = var.enable_ipv6 ? aws_subnet.public[*].ipv6_cidr_block : []
}

output "ecs_security_group_id" {
  description = "ID of the security group for ECS tasks"
  value       = aws_security_group.ecs_tasks.id
}

output "egress_only_internet_gateway_id" {
  description = "ID of the egress-only internet gateway when IPv6 is enabled"
  value       = var.enable_ipv6 ? aws_egress_only_internet_gateway.this[0].id : null
}

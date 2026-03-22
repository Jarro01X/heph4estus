output "instance_profile_arn" {
  description = "ARN of the IAM instance profile for spot workers"
  value       = aws_iam_instance_profile.spot_worker.arn
}

output "ami_id" {
  description = "AMI ID for spot instances (Amazon Linux 2023)"
  value       = data.aws_ami.al2023.id
}

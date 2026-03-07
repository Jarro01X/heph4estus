data "aws_caller_identity" "current" {}
data "aws_region" "current" {}

# Latest Amazon Linux 2023 AMI
data "aws_ami" "al2023" {
  most_recent = true
  owners      = ["amazon"]

  filter {
    name   = "name"
    values = ["al2023-ami-*-x86_64"]
  }

  filter {
    name   = "virtualization-type"
    values = ["hvm"]
  }
}

# IAM role for spot instances — same permissions as ECS task role
# plus ECR pull (no execution role on raw EC2) and self-terminate.
resource "aws_iam_role" "spot_worker" {
  name = "${var.name_prefix}-spot-worker-role"

  assume_role_policy = jsonencode({
    Version = "2012-10-17"
    Statement = [{
      Action = "sts:AssumeRole"
      Effect = "Allow"
      Principal = {
        Service = "ec2.amazonaws.com"
      }
    }]
  })

  tags = {
    Name        = "${var.name_prefix}-spot-worker-role"
    Environment = var.environment
    Terraform   = "true"
  }
}

resource "aws_iam_instance_profile" "spot_worker" {
  name = "${var.name_prefix}-spot-worker-profile"
  role = aws_iam_role.spot_worker.name
}

resource "aws_iam_policy" "spot_worker" {
  name        = "${var.name_prefix}-spot-worker-policy"
  description = "Permissions for spot worker instances"

  policy = jsonencode({
    Version = "2012-10-17"
    Statement = [
      {
        Sid    = "SQSAccess"
        Effect = "Allow"
        Action = [
          "sqs:ReceiveMessage",
          "sqs:DeleteMessage",
          "sqs:GetQueueAttributes"
        ]
        Resource = var.sqs_queue_arn
      },
      {
        Sid    = "S3Write"
        Effect = "Allow"
        Action = [
          "s3:PutObject"
        ]
        Resource = "${var.s3_bucket_arn}/*"
      },
      {
        Sid    = "ECRPull"
        Effect = "Allow"
        Action = [
          "ecr:GetAuthorizationToken",
          "ecr:BatchCheckLayerAvailability",
          "ecr:GetDownloadUrlForLayer",
          "ecr:BatchGetImage"
        ]
        Resource = "*"
      },
      {
        Sid    = "SelfTerminate"
        Effect = "Allow"
        Action = [
          "ec2:TerminateInstances"
        ]
        Resource = "arn:aws:ec2:${data.aws_region.current.name}:${data.aws_caller_identity.current.account_id}:instance/*"
        Condition = {
          StringEquals = {
            "ec2:ResourceTag/Project" = "heph4estus"
          }
        }
      }
    ]
  })
}

resource "aws_iam_role_policy_attachment" "spot_worker" {
  role       = aws_iam_role.spot_worker.name
  policy_arn = aws_iam_policy.spot_worker.arn
}

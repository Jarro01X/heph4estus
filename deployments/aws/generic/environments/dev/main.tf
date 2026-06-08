terraform {
  required_version = ">= 1.5.0"

  required_providers {
    aws = {
      source  = "hashicorp/aws"
      version = "~> 6.0"
    }
  }
}

provider "aws" {
  region = var.aws_region
}

data "aws_caller_identity" "current" {}

locals {
  environment = "dev"
}

# Customer-managed key for AWS service encryption in the dev environment
resource "aws_kms_key" "infrastructure" {
  description             = "${var.name_prefix} AWS infrastructure encryption"
  deletion_window_in_days = 7
  enable_key_rotation     = true

  policy = jsonencode({
    Version = "2012-10-17"
    Statement = [
      {
        Sid    = "EnableAccountAdministration"
        Effect = "Allow"
        Principal = {
          AWS = "arn:aws:iam::${data.aws_caller_identity.current.account_id}:root"
        }
        Action   = "kms:*"
        Resource = "*"
      },
      {
        Sid    = "AllowCloudWatchLogsUse"
        Effect = "Allow"
        Principal = {
          Service = "logs.${var.aws_region}.amazonaws.com"
        }
        Action = [
          "kms:Encrypt",
          "kms:Decrypt",
          "kms:ReEncrypt*",
          "kms:GenerateDataKey*",
          "kms:DescribeKey"
        ]
        Resource = "*"
        Condition = {
          ArnLike = {
            "kms:EncryptionContext:aws:logs:arn" = "arn:aws:logs:${var.aws_region}:${data.aws_caller_identity.current.account_id}:log-group:*"
          }
        }
      },
      {
        Sid    = "AllowIntegratedServiceUse"
        Effect = "Allow"
        Principal = {
          AWS = "arn:aws:iam::${data.aws_caller_identity.current.account_id}:root"
        }
        Action = [
          "kms:CreateGrant",
          "kms:Encrypt",
          "kms:Decrypt",
          "kms:ReEncrypt*",
          "kms:GenerateDataKey*",
          "kms:DescribeKey",
          "kms:ListGrants",
          "kms:RetireGrant"
        ]
        Resource = "*"
        Condition = {
          StringEquals = {
            "kms:CallerAccount" = data.aws_caller_identity.current.account_id
            "kms:ViaService" = [
              "ecr.${var.aws_region}.amazonaws.com",
              "s3.${var.aws_region}.amazonaws.com",
              "sqs.${var.aws_region}.amazonaws.com"
            ]
          }
        }
      }
    ]
  })

  tags = {
    Name        = "${var.name_prefix}-infrastructure-key"
    Environment = local.environment
    Terraform   = "true"
  }
}

resource "aws_kms_alias" "infrastructure" {
  name          = "alias/${var.name_prefix}-infrastructure"
  target_key_id = aws_kms_key.infrastructure.key_id
}

# Create network infrastructure
module "networking" {
  source = "../../../shared/networking"

  vpc_cidr    = var.vpc_cidr
  az_count    = var.az_count
  name_prefix = var.name_prefix
  environment = local.environment
  enable_ipv6 = var.enable_ipv6
  multi_nat   = var.multi_nat
}

# Create storage for results
module "storage" {
  source = "../../storage"

  name_prefix            = var.name_prefix
  environment            = local.environment
  force_destroy_bucket   = true # Make it easier to clean up in dev
  kms_key_arn            = aws_kms_key.infrastructure.arn
  results_retention_days = 30 # Only keep results for 30 days in dev
}

# Create messaging infrastructure
module "messaging" {
  source = "../../messaging"

  name_prefix = var.name_prefix
  environment = local.environment
  kms_key_arn = aws_kms_key.infrastructure.arn
}

# Create security roles and policies
module "security" {
  source = "../../../shared/security"

  name_prefix   = var.name_prefix
  environment   = local.environment
  kms_key_arn   = aws_kms_key.infrastructure.arn
  sqs_queue_arn = module.messaging.queue_arn
  s3_bucket_arn = module.storage.bucket_arn
}

# Create compute resources
module "compute" {
  source = "../../compute"

  name_prefix            = var.name_prefix
  environment            = local.environment
  aws_region             = var.aws_region
  image_tag              = var.image_tag
  log_retention_days     = var.log_retention_days
  kms_key_arn            = aws_kms_key.infrastructure.arn
  task_cpu               = var.task_cpu
  task_memory            = var.task_memory
  ecs_execution_role_arn = module.security.ecs_execution_role_arn
  ecs_task_role_arn      = module.security.ecs_task_role_arn
  sqs_queue_url          = module.messaging.queue_url
  s3_bucket_id           = module.storage.bucket_id
  tool_name              = var.tool_name
  jitter_max_seconds     = var.jitter_max_seconds
  container_env_vars     = var.container_env_vars
}

# Create spot instance prerequisites (IAM + AMI lookup)
module "spot" {
  source = "../../spot"

  name_prefix   = var.name_prefix
  environment   = local.environment
  sqs_queue_arn = module.messaging.queue_arn
  s3_bucket_arn = module.storage.bucket_arn
}

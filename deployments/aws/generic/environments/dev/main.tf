provider "aws" {
  region = var.aws_region
}

locals {
  environment = "dev"
}

# Create network infrastructure
module "networking" {
  source = "../../../shared/networking"

  vpc_cidr    = var.vpc_cidr
  az_count    = var.az_count
  name_prefix = var.name_prefix
  environment = local.environment
}

# Create storage for results
module "storage" {
  source = "../../storage"

  name_prefix            = var.name_prefix
  environment            = local.environment
  force_destroy_bucket   = true # Make it easier to clean up in dev
  results_retention_days = 30   # Only keep results for 30 days in dev
}

# Create messaging infrastructure
module "messaging" {
  source = "../../messaging"

  name_prefix = var.name_prefix
  environment = local.environment
}

# Create security roles and policies
module "security" {
  source = "../../../shared/security"

  name_prefix   = var.name_prefix
  environment   = local.environment
  sqs_queue_arn = module.messaging.queue_arn
  s3_bucket_arn = module.storage.bucket_arn
}

# Create compute resources
module "compute" {
  source = "../../compute"

  name_prefix            = var.name_prefix
  environment            = local.environment
  aws_region             = var.aws_region
  log_retention_days     = var.log_retention_days
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

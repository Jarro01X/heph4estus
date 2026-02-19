provider "aws" {
  region = var.aws_region
}

locals {
  environment = "dev"
}

# Create network infrastructure
module "networking" {
  source = "../../../shared/networking"

  vpc_cidr     = var.vpc_cidr
  az_count     = var.az_count
  name_prefix  = var.name_prefix
  environment  = local.environment
}

# Create storage for scan results
module "storage" {
  source = "../storage"

  name_prefix           = var.name_prefix
  environment           = local.environment
  force_destroy_bucket  = true  # Make it easier to clean up in dev
  results_retention_days = 30   # Only keep results for 30 days in dev
}

# Create messaging infrastructure
module "messaging" {
  source = "../messaging"

  name_prefix  = var.name_prefix
  environment  = local.environment
}

# Create security roles and policies
module "security" {
  source = "../../../shared/security"

  name_prefix    = var.name_prefix
  environment    = local.environment
  sqs_queue_arn  = module.messaging.queue_arn
  s3_bucket_arn  = module.storage.bucket_arn
}

# Create compute resources
module "compute" {
  source = "../compute"

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
}

# Create workflow orchestration
module "orchestration" {
  source = "../orchestration"

  name_prefix              = var.name_prefix
  environment              = local.environment
  step_functions_role_arn  = module.security.step_functions_role_arn
  sqs_queue_url            = module.messaging.queue_url
  ecs_cluster_arn          = module.compute.ecs_cluster_arn
  ecs_task_definition_arn  = module.compute.ecs_task_definition_arn
  private_subnet_ids       = module.networking.private_subnet_ids
  ecs_security_group_id    = module.networking.ecs_security_group_id
  log_retention_days       = var.log_retention_days
  max_concurrency          = var.max_concurrency
}
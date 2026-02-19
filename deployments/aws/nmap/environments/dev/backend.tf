terraform {
  # Use S3 backend for state storage. Uncomment and configure when ready.
  # backend "s3" {
  #   bucket         = "heph4estus-dev-tfstate"
  #   key            = "terraform.tfstate"
  #   region         = "us-east-1"
  #   dynamodb_table = "heph4estus-dev-tfstate-lock"
  #   encrypt        = true
  # }
}
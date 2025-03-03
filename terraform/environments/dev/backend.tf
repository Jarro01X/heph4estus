terraform {
  # Use S3 backend for state storage. Uncomment and configure when ready.
  # backend "s3" {
  #   bucket         = "nmap-scanner-dev-tfstate"
  #   key            = "terraform.tfstate"
  #   region         = "us-east-1"
  #   dynamodb_table = "nmap-scanner-dev-tfstate-lock"
  #   encrypt        = true
  # }
}
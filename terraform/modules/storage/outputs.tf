output "bucket_id" {
  description = "ID of the S3 bucket for scan results"
  value       = aws_s3_bucket.results.id
}

output "bucket_arn" {
  description = "ARN of the S3 bucket for scan results"
  value       = aws_s3_bucket.results.arn
}

output "bucket_domain_name" {
  description = "Domain name of the S3 bucket"
  value       = aws_s3_bucket.results.bucket_domain_name
}
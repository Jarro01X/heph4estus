# Heph4estus - Scaling red team tooling with cloud infrastructure

## Important Notes

- This project is designed for legitimate security testing only. Ensure you have permission to scan the targets.
- Scan costs depend on the number and duration of scans run.
- The default configuration launches Fargate tasks with 0.25 vCPU and 0.5GB memory.
- Max concurrency at the moment is 10 ECS Tasks.

## Project Overview

Heph4estus is a CLI app that takes care of creating cloud infrastructure and handling data for red team tools. For an in-depth explanation of the architecture, roadmap, and the project as a whole please check [hephaestus.tools](https://www.hephaestus.tools).

## Requirements

### Software Requirements

- **Go 1.21+**: For building and running the producer/consumer applications
- **Docker**: For building and pushing container images
- **Terraform 1.0+**: For deploying the infrastructure
- **AWS CLI**: Configured with appropriate credentials and permissions
- **Nmap**: Already included in the container image for scanning

## Setup Instructions

### 1. Clone the Repository

```bash
git clone <repository-url>
cd heph4estus
```

### 2. Deploy Infrastructure with Terraform

```bash
# Initialize Terraform
cd terraform/environments/dev
terraform init

# Review the planned changes
terraform plan

# Apply the changes
terraform apply
```

### 3. Build and Push the Container Image

Once Terraform finish instatiating the necessary infrastructure it will output specific arns and urls that will be crucial for the following steps

```bash
# Build the Docker image
docker build -t nmap-scanner .

# Please copy the ecr_repositopry_url output and paste it here
ECR_REPO=repo.url.example

# Please copy everything that comes before the / in the ecr_repository_url output and overwrite repo.url.example with it
ECR_REGISTRY=registtry.repo.com

# Login to ECR
aws ecr get-login-password --region us-east-1 | docker login --username AWS --password-stdin $ECR_REGISTRY

# Tag and push the image
docker tag nmap-scanner:latest $ECR_REPO:latest
docker push $ECR_REPO:latest
```

### 4. Create a Targets File

Create a file named `targets.txt` with a list of targets to scan, one per line:

```
example.com -sV -p 80,443
10.0.0.0/24 -sS -p 22
192.168.1.1 -A
```

Format: `<target> [nmap options]`

If no options are specified, the default options (`-sS`) will be used. Currently the project only has support for scanning targets that are open to the internet.

### 5. Build the Producer Application

```bash
# Build the producer
go build -o bin/producer cmd/producer/main.go
```

## Running the Scanner

### Start Scanning

```bash
# Please copy the state_machine_arn and paste it here
export STATE_MACHINE_ARN=arn.example.aws

# Run the producer with your targets file
./bin/producer -file targets.txt
```

### Viewing Results

Scan results are stored in JSON format in the S3 bucket created by Terraform:

```bash
# Please copy the s3_bucket_name in the output and paste it below
S3_BUCKET=s3.bucketname.holder

# List scan results
aws s3 ls s3://$S3_BUCKET/scans/

# Download a specific result
aws s3 cp s3://$S3_BUCKET/scans/example.com_1646347520.json .
```

## Clean Up
Go to your S3 and delete the files currently inside, then run the following:

```bash
cd terraform/environments/dev
terraform destroy
```

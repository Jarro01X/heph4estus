# Linode fleet module — provisions a controller VM (NATS + MinIO + registry
# via the selfhosted controller module) and N worker VMs that auto-join the
# fleet on boot via cloud-init.
#
# The controller module generates credentials and cloud-init; this module
# adds all Linode-specific resources (instances, networking, firewall).

terraform {
  required_version = ">= 1.3"

  required_providers {
    linode = {
      source  = "linode/linode"
      version = ">= 2.0"
    }
  }
}

provider "linode" {
  token = var.linode_token
}

# --- Generation ID ---

resource "random_id" "generation" {
  byte_length = 8
}

# --- Random root password (SSH keys are the actual access method) ---

resource "random_password" "root_pass" {
  length  = 32
  special = false
}

locals {
  generation_id          = var.generation_id != "" ? var.generation_id : random_id.generation.hex
  controller_private_ip  = "10.0.1.2"
  controller_host        = module.controller.controller_host
  nats_operator_user     = var.nats_operator_user_override != "" ? var.nats_operator_user_override : module.controller.nats_operator_user
  nats_operator_password = var.nats_operator_password_override != "" ? var.nats_operator_password_override : module.controller.nats_operator_password
  nats_worker_user       = var.nats_worker_user_override != "" ? var.nats_worker_user_override : module.controller.nats_worker_user
  nats_worker_password   = var.nats_worker_password_override != "" ? var.nats_worker_password_override : module.controller.nats_worker_password
  minio_operator_access_key = (
    var.minio_operator_access_key_override != "" ? var.minio_operator_access_key_override : module.controller.s3_operator_access_key
  )
  minio_operator_secret_key = (
    var.minio_operator_secret_key_override != "" ? var.minio_operator_secret_key_override : module.controller.s3_operator_secret_key
  )
  minio_worker_access_key = (
    var.minio_worker_access_key_override != "" ? var.minio_worker_access_key_override : module.controller.s3_worker_access_key
  )
  minio_worker_secret_key = (
    var.minio_worker_secret_key_override != "" ? var.minio_worker_secret_key_override : module.controller.s3_worker_secret_key
  )
  registry_publisher_username = var.registry_publisher_username_override != "" ? var.registry_publisher_username_override : module.controller.registry_publisher_username
  registry_publisher_password = var.registry_publisher_password_override != "" ? var.registry_publisher_password_override : module.controller.registry_publisher_password
  registry_worker_username    = var.registry_worker_username_override != "" ? var.registry_worker_username_override : module.controller.registry_worker_username
  registry_worker_password    = var.registry_worker_password_override != "" ? var.registry_worker_password_override : module.controller.registry_worker_password
}

# --- Networking (VPC + subnet) ---

resource "linode_vpc" "fleet" {
  label  = "heph-fleet"
  region = var.region
}

resource "linode_vpc_subnet" "nodes" {
  vpc_id = linode_vpc.fleet.id
  label  = "heph-nodes"
  ipv4   = "10.0.1.0/24"
}

# --- Firewall ---

resource "linode_firewall" "fleet" {
  label = "heph-fleet-fw"

  # Inbound: SSH from anywhere
  inbound {
    label    = "ssh"
    action   = "ACCEPT"
    protocol = "TCP"
    ports    = "22"
    ipv4     = ["0.0.0.0/0"]
    ipv6     = ["::/0"]
  }

  # Inbound: NATS (4222) — restricted to private network.
  # Workers access via VPC. Operator access requires NATS auth credentials.
  inbound {
    label    = "nats"
    action   = "ACCEPT"
    protocol = "TCP"
    ports    = "4222"
    ipv4     = ["10.0.1.0/24"]
  }

  # Inbound: Docker registry (5000) — private network only.
  inbound {
    label    = "registry"
    action   = "ACCEPT"
    protocol = "TCP"
    ports    = "5000"
    ipv4     = ["10.0.1.0/24"]
  }

  # Inbound: MinIO S3 API (9000) — private network only.
  inbound {
    label    = "minio"
    action   = "ACCEPT"
    protocol = "TCP"
    ports    = "9000"
    ipv4     = ["10.0.1.0/24"]
  }

  # Default policies
  inbound_policy  = "DROP"
  outbound_policy = "ACCEPT"

  linodes = concat(
    [linode_instance.controller.id],
    linode_instance.worker[*].id,
  )
}

# --- Controller module (credentials + cloud-init) ---

module "controller" {
  source = "../selfhosted/controller"

  # The controller module uses controller_ip only in its outputs (not in
  # cloud-init). We override those outputs in this module's outputs.tf
  # with the real public IP from the Linode instance.
  controller_ip = "0.0.0.0"
  tool_name     = var.tool_name
  minio_bucket  = var.minio_bucket

  controller_security_mode           = var.controller_security_mode
  controller_ca_pem_override         = var.controller_ca_pem_override
  controller_cert_pem_override       = var.controller_cert_pem_override
  controller_key_pem_override        = var.controller_key_pem_override
  controller_cert_not_after_override = var.controller_cert_not_after_override
  controller_cert_generation         = var.controller_cert_generation
  controller_cert_rotated_at         = var.controller_cert_rotated_at
}

# --- Controller Instance ---

resource "linode_instance" "controller" {
  label  = "heph-controller"
  region = var.region
  type   = var.controller_type
  image  = "linode/ubuntu24.04"

  root_pass       = random_password.root_pass.result
  authorized_keys = [trimspace(var.ssh_public_key)]

  metadata {
    user_data = base64encode(module.controller.cloud_init)
  }

  interface {
    purpose = "public"
  }

  interface {
    purpose   = "vpc"
    subnet_id = linode_vpc_subnet.nodes.id
    ipv4 {
      vpc = local.controller_private_ip
    }
  }

  depends_on = [linode_vpc_subnet.nodes]
}

# --- Worker cloud-init ---

locals {
  worker_cloud_init = [
    for i in range(var.worker_count) : templatefile("${path.module}/templates/worker-cloud-init.yaml", {
      controller_private_ip = local.controller_private_ip
      controller_host       = local.controller_host
      nats_port             = 4222
      nats_scheme           = module.controller.nats_tls_enabled ? "tls" : "nats"
      nats_subject          = module.controller.nats_stream
      nats_user             = local.nats_worker_user
      nats_password         = local.nats_worker_password
      minio_port            = 9000
      minio_scheme          = module.controller.minio_tls_enabled ? "https" : "http"
      minio_access_key      = local.minio_worker_access_key
      minio_secret_key      = local.minio_worker_secret_key
      minio_bucket          = var.minio_bucket
      registry_port         = 5000
      registry_scheme       = module.controller.registry_tls_enabled ? "https" : "http"
      registry_tls_enabled  = module.controller.registry_tls_enabled
      registry_username     = local.registry_worker_username
      registry_password     = local.registry_worker_password
      controller_ca_pem_b64 = base64encode(module.controller.controller_ca_pem)
      tool_name             = var.tool_name
      docker_image          = var.docker_image
      generation_id         = local.generation_id
      worker_index          = i
      worker_private_ip     = "10.0.1.${i + 10}"
    })
  ]
}

# --- Worker Instances ---

resource "linode_instance" "worker" {
  count = var.worker_count

  label  = "heph-worker-${count.index}"
  region = var.region
  type   = var.worker_type
  image  = "linode/ubuntu24.04"

  root_pass       = random_password.root_pass.result
  authorized_keys = [trimspace(var.ssh_public_key)]

  metadata {
    user_data = base64encode(local.worker_cloud_init[count.index])
  }

  interface {
    purpose = "public"
  }

  interface {
    purpose   = "vpc"
    subnet_id = linode_vpc_subnet.nodes.id
    ipv4 {
      vpc = "10.0.1.${count.index + 10}"
    }
  }

  depends_on = [linode_vpc_subnet.nodes, linode_instance.controller]
}

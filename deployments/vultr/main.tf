# Vultr fleet module — provisions a controller VM (NATS + MinIO + registry
# via the selfhosted controller module) and N worker VMs that auto-join the
# fleet on boot via cloud-init.
#
# The controller module generates credentials and cloud-init; this module
# adds all Vultr-specific resources (instances, networking, firewall).

terraform {
  required_version = ">= 1.3"

  required_providers {
    vultr = {
      source  = "vultr/vultr"
      version = ">= 2.19"
    }
  }
}

provider "vultr" {
  api_key = var.vultr_api_key
}

# --- Generation ID ---

resource "random_id" "generation" {
  byte_length = 8
}

locals {
  generation_id = var.generation_id != "" ? var.generation_id : random_id.generation.hex
}

# --- OS lookup ---

data "vultr_os" "ubuntu" {
  filter {
    name   = "name"
    values = ["Ubuntu 24.04 LTS x64"]
  }
}

# --- SSH Key ---

resource "vultr_ssh_key" "deploy" {
  name    = var.ssh_key_name
  ssh_key = trimspace(var.ssh_public_key)
}

# --- Networking (VPC 2.0) ---

resource "vultr_vpc2" "fleet" {
  description   = "heph-fleet"
  region        = var.region
  ip_type       = "v4"
  ip_block      = "10.0.1.0"
  prefix_length = 24
}

# --- Firewall ---

resource "vultr_firewall_group" "fleet" {
  description = "heph-fleet-fw"
}

# Inbound: SSH (IPv4 + IPv6)
resource "vultr_firewall_rule" "ssh_v4" {
  firewall_group_id = vultr_firewall_group.fleet.id
  protocol          = "tcp"
  ip_type           = "v4"
  subnet            = "0.0.0.0"
  subnet_size       = 0
  port              = "22"
}

resource "vultr_firewall_rule" "ssh_v6" {
  firewall_group_id = vultr_firewall_group.fleet.id
  protocol          = "tcp"
  ip_type           = "v6"
  subnet            = "::"
  subnet_size       = 0
  port              = "22"
}

# Inbound: NATS client port (IPv4 + IPv6)
resource "vultr_firewall_rule" "nats_v4" {
  firewall_group_id = vultr_firewall_group.fleet.id
  protocol          = "tcp"
  ip_type           = "v4"
  subnet            = "0.0.0.0"
  subnet_size       = 0
  port              = "4222"
}

resource "vultr_firewall_rule" "nats_v6" {
  firewall_group_id = vultr_firewall_group.fleet.id
  protocol          = "tcp"
  ip_type           = "v6"
  subnet            = "::"
  subnet_size       = 0
  port              = "4222"
}

# Inbound: Docker registry (IPv4 + IPv6)
resource "vultr_firewall_rule" "registry_v4" {
  firewall_group_id = vultr_firewall_group.fleet.id
  protocol          = "tcp"
  ip_type           = "v4"
  subnet            = "0.0.0.0"
  subnet_size       = 0
  port              = "5000"
}

resource "vultr_firewall_rule" "registry_v6" {
  firewall_group_id = vultr_firewall_group.fleet.id
  protocol          = "tcp"
  ip_type           = "v6"
  subnet            = "::"
  subnet_size       = 0
  port              = "5000"
}

# Inbound: MinIO S3 API (IPv4 + IPv6)
resource "vultr_firewall_rule" "minio_v4" {
  firewall_group_id = vultr_firewall_group.fleet.id
  protocol          = "tcp"
  ip_type           = "v4"
  subnet            = "0.0.0.0"
  subnet_size       = 0
  port              = "9000"
}

resource "vultr_firewall_rule" "minio_v6" {
  firewall_group_id = vultr_firewall_group.fleet.id
  protocol          = "tcp"
  ip_type           = "v6"
  subnet            = "::"
  subnet_size       = 0
  port              = "9000"
}

# --- Controller module (credentials + cloud-init) ---

module "controller" {
  source = "../selfhosted/controller"

  # The controller module uses controller_ip only in its outputs (not in
  # cloud-init). We override those outputs in this module's outputs.tf
  # with the real public IP from the Vultr instance.
  controller_ip = "0.0.0.0"
  tool_name     = var.tool_name
  minio_bucket  = var.minio_bucket
}

# --- Controller Instance ---

resource "vultr_instance" "controller" {
  label             = "heph-controller"
  region            = var.region
  plan              = var.controller_plan
  os_id             = data.vultr_os.ubuntu.id
  enable_ipv6       = true
  firewall_group_id = vultr_firewall_group.fleet.id
  ssh_key_ids       = [vultr_ssh_key.deploy.id]
  vpc2_ids          = [vultr_vpc2.fleet.id]
  user_data         = module.controller.cloud_init

  hostname = "heph-controller"

  depends_on = [vultr_vpc2.fleet]
}

# --- Worker cloud-init ---

locals {
  worker_cloud_init = [
    for i in range(var.worker_count) : templatefile("${path.module}/templates/worker-cloud-init.yaml", {
      controller_private_ip = vultr_instance.controller.internal_ip
      nats_port             = 4222
      nats_subject          = module.controller.nats_stream
      minio_port            = 9000
      minio_access_key      = module.controller.s3_access_key
      minio_secret_key      = module.controller.s3_secret_key
      minio_bucket          = var.minio_bucket
      registry_port         = 5000
      tool_name             = var.tool_name
      docker_image          = var.docker_image
      generation_id         = local.generation_id
      worker_index          = i
      worker_private_ip     = "auto"
    })
  ]
}

# --- Worker Instances ---

resource "vultr_instance" "worker" {
  count = var.worker_count

  label             = "heph-worker-${count.index}"
  region            = var.region
  plan              = var.worker_plan
  os_id             = data.vultr_os.ubuntu.id
  enable_ipv6       = true
  firewall_group_id = vultr_firewall_group.fleet.id
  ssh_key_ids       = [vultr_ssh_key.deploy.id]
  vpc2_ids          = [vultr_vpc2.fleet.id]
  user_data         = local.worker_cloud_init[count.index]

  hostname = "heph-worker-${count.index}"

  depends_on = [vultr_vpc2.fleet, vultr_instance.controller]
}

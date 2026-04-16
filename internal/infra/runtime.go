package infra

import "heph4estus/internal/cloud"

// AWSRequiredOutputKeys lists the Terraform output keys that must be present
// for an AWS scan to proceed. This includes spot-mode keys (ami_id,
// instance_profile_arn) because the generic Terraform module always outputs
// them, and their absence indicates stale or partial infrastructure.
// tool_name is required to detect mismatches.
var AWSRequiredOutputKeys = []string{
	"tool_name",
	"sqs_queue_url",
	"s3_bucket_name",
	"ecr_repo_url",
	"ecs_cluster_name",
	"task_definition_arn",
	"subnet_ids",
	"security_group_id",
	"ami_id",
	"instance_profile_arn",
}

// SelfhostedRequiredOutputKeys lists the output keys for selfhosted
// infrastructure. Selfhosted does not use Terraform for provisioning so this
// is intentionally minimal — only tool_name is required to enable lifecycle
// mismatch detection. Later tracks may extend this if selfhosted gains its
// own state-file contract.
var SelfhostedRequiredOutputKeys = []string{
	"tool_name",
}

// HetznerRequiredOutputKeys lists the Terraform output keys that must be
// present for a Hetzner deploy to be considered ready. These are produced
// by deployments/hetzner/ and include both controller endpoints and worker
// metadata.
var HetznerRequiredOutputKeys = []string{
	"tool_name",
	"cloud",
	"nats_url",
	"nats_stream",
	"s3_endpoint",
	"s3_access_key",
	"s3_secret_key",
	"s3_bucket_name",
	"registry_url",
	"docker_image",
	"sqs_queue_url",
	"controller_ip",
	"generation_id",
	"worker_count",
	"worker_hosts",
}

// LinodeRequiredOutputKeys lists the Terraform output keys that must be
// present for a Linode deploy to be considered ready. The output contract
// mirrors Hetzner — both provider-native VPS paths produce the same key
// set so the lifecycle/factory code works uniformly.
var LinodeRequiredOutputKeys = []string{
	"tool_name",
	"cloud",
	"nats_url",
	"nats_stream",
	"s3_endpoint",
	"s3_access_key",
	"s3_secret_key",
	"s3_bucket_name",
	"registry_url",
	"docker_image",
	"sqs_queue_url",
	"controller_ip",
	"generation_id",
	"worker_count",
	"worker_hosts",
}

// VultrRequiredOutputKeys lists the Terraform output keys that must be
// present for a Vultr deploy to be considered ready. The output contract
// mirrors Hetzner and Linode — all provider-native VPS paths produce the
// same key set so the lifecycle/factory code works uniformly.
var VultrRequiredOutputKeys = []string{
	"tool_name",
	"cloud",
	"nats_url",
	"nats_stream",
	"s3_endpoint",
	"s3_access_key",
	"s3_secret_key",
	"s3_bucket_name",
	"registry_url",
	"docker_image",
	"sqs_queue_url",
	"controller_ip",
	"generation_id",
	"worker_count",
	"worker_hosts",
}

// RequiredOutputKeysForCloud returns the required output keys for the given
// cloud provider family. Unknown kinds fall back to the AWS set.
func RequiredOutputKeysForCloud(kind cloud.Kind) []string {
	switch kind.Canonical() {
	case cloud.KindHetzner:
		return HetznerRequiredOutputKeys
	case cloud.KindLinode:
		return LinodeRequiredOutputKeys
	case cloud.KindVultr:
		return VultrRequiredOutputKeys
	default:
		if kind.IsSelfhostedFamily() {
			return SelfhostedRequiredOutputKeys
		}
		return AWSRequiredOutputKeys
	}
}

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

// RequiredOutputKeysForCloud returns the required output keys for the given
// cloud provider family. Unknown kinds fall back to the AWS set.
func RequiredOutputKeysForCloud(kind cloud.Kind) []string {
	if kind.IsSelfhostedFamily() {
		return SelfhostedRequiredOutputKeys
	}
	return AWSRequiredOutputKeys
}

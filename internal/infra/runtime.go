package infra

import "heph4estus/internal/cloud"

// AWSRequiredOutputKeys lists the Terraform output keys that must be present
// for an AWS scan to proceed. This includes spot-mode keys (ami_id,
// instance_profile_arn) because the generic Terraform module always outputs
// them, and their absence indicates stale or partial infrastructure.
// tool_name is required to detect mismatches.
var AWSRequiredOutputKeys = []string{
	OutputToolName,
	OutputSQSQueueURL,
	OutputS3BucketName,
	OutputECRRepoURL,
	OutputImageTag,
	OutputDockerImage,
	OutputECSClusterName,
	OutputTaskDefinitionARN,
	OutputSubnetIDs,
	OutputSecurityGroupID,
	OutputAMIID,
	OutputInstanceProfile,
}

// SelfhostedRequiredOutputKeys lists the output keys for selfhosted
// infrastructure. Selfhosted does not use Terraform for provisioning so this
// is intentionally minimal — only tool_name is required to enable lifecycle
// mismatch detection. Later tracks may extend this if selfhosted gains its
// own state-file contract.
var SelfhostedRequiredOutputKeys = []string{
	OutputToolName,
}

// ProviderNativeRequiredOutputKeys lists the Terraform output keys that must
// be present for Hetzner, Linode, and Vultr deploys to be considered ready.
// These providers share the same selfhosted runtime contract.
var ProviderNativeRequiredOutputKeys = []string{
	OutputToolName,
	OutputCloud,
	OutputControllerSecurityMode,
	OutputCredentialScopeVersion,
	OutputNATSURL,
	OutputNATSStream,
	OutputNATSUser,
	OutputNATSPassword,
	OutputNATSOperatorUser,
	OutputNATSOperatorPassword,
	OutputNATSTLSEnabled,
	OutputNATSMTLSEnabled,
	OutputNATSAuthEnabled,
	OutputNATSOperatorClientCertPEM,
	OutputNATSOperatorClientCertNotAfter,
	OutputNATSOperatorClientKeyPEM,
	OutputNATSWorkerClientCertNotAfter,
	OutputS3Endpoint,
	OutputS3AccessKey,
	OutputS3SecretKey,
	OutputS3OperatorAccessKey,
	OutputS3OperatorSecretKey,
	OutputS3BucketName,
	OutputMinIOTLSEnabled,
	OutputMinIOAuthEnabled,
	OutputRegistryURL,
	OutputRegistryUsername,
	OutputRegistryPassword,
	OutputRegistryPublisherUsername,
	OutputRegistryPublisherPassword,
	OutputRegistryTLSEnabled,
	OutputRegistryAuthEnabled,
	OutputControllerCAPEM,
	OutputControllerCAFingerprintSHA256,
	OutputControllerCertNotAfter,
	OutputControllerHost,
	OutputDockerImage,
	OutputSQSQueueURL,
	OutputControllerIP,
	OutputGenerationID,
	OutputWorkerCount,
	OutputWorkerHosts,
}

// RequiredOutputKeysForCloud returns the required output keys for the given
// cloud provider family. Unknown kinds fall back to the AWS set.
func RequiredOutputKeysForCloud(kind cloud.Kind) []string {
	switch kind.Canonical() {
	case cloud.KindHetzner, cloud.KindLinode, cloud.KindVultr:
		return ProviderNativeRequiredOutputKeys
	default:
		if kind.IsSelfhostedFamily() {
			return SelfhostedRequiredOutputKeys
		}
		return AWSRequiredOutputKeys
	}
}

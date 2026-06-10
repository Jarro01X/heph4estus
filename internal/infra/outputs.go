package infra

import (
	"strconv"
	"strings"

	"heph4estus/internal/cloud"
)

const (
	OutputToolName = "tool_name"
	OutputCloud    = "cloud"

	OutputSQSQueueURL       = "sqs_queue_url"
	OutputECRRepoURL        = "ecr_repo_url"
	OutputImageTag          = "image_tag"
	OutputDockerImage       = "docker_image"
	OutputS3BucketName      = "s3_bucket_name"
	OutputS3Endpoint        = "s3_endpoint"
	OutputS3Region          = "s3_region"
	OutputS3AccessKey       = "s3_access_key"
	OutputS3SecretKey       = "s3_secret_key"
	OutputS3PathStyle       = "s3_path_style"
	OutputECSClusterName    = "ecs_cluster_name"
	OutputTaskDefinitionARN = "task_definition_arn"
	OutputSubnetIDs         = "subnet_ids"
	OutputSecurityGroupID   = "security_group_id"
	OutputInstanceProfile   = "instance_profile_arn"
	OutputAMIID             = "ami_id"

	OutputControllerSecurityMode = "controller_security_mode"
	OutputCredentialScopeVersion = "credential_scope_version"
	OutputNATSURL                = "nats_url"
	OutputNATSStream             = "nats_stream"
	OutputNATSUser               = "nats_user"
	OutputNATSPassword           = "nats_password"
	OutputNATSOperatorUser       = "nats_operator_user"
	OutputNATSOperatorPassword   = "nats_operator_password"
	OutputNATSTLSEnabled         = "nats_tls_enabled"
	OutputNATSMTLSEnabled        = "nats_mtls_enabled"
	OutputNATSAuthEnabled        = "nats_auth_enabled"

	OutputNATSOperatorClientCertPEM      = "nats_operator_client_cert_pem"
	OutputNATSOperatorClientCertNotAfter = "nats_operator_client_cert_not_after"
	OutputNATSOperatorClientKeyPEM       = "nats_operator_client_key_pem"
	OutputNATSWorkerClientCertNotAfter   = "nats_worker_client_cert_not_after"

	OutputS3OperatorAccessKey = "s3_operator_access_key"
	OutputS3OperatorSecretKey = "s3_operator_secret_key"
	OutputMinIOTLSEnabled     = "minio_tls_enabled"
	OutputMinIOAuthEnabled    = "minio_auth_enabled"

	OutputRegistryURL               = "registry_url"
	OutputRegistryUsername          = "registry_username"
	OutputRegistryPassword          = "registry_password"
	OutputRegistryPublisherUsername = "registry_publisher_username"
	OutputRegistryPublisherPassword = "registry_publisher_password"
	OutputRegistryTLSEnabled        = "registry_tls_enabled"
	OutputRegistryAuthEnabled       = "registry_auth_enabled"

	OutputControllerCAPEM               = "controller_ca_pem"
	OutputControllerCAFingerprintSHA256 = "controller_ca_fingerprint_sha256"
	OutputControllerCertNotAfter        = "controller_cert_not_after"
	OutputControllerHost                = "controller_host"
	OutputControllerIP                  = "controller_ip"
	OutputControllerIPv6                = "controller_ipv6"
	OutputGenerationID                  = "generation_id"

	OutputWorkerCount      = "worker_count"
	OutputWorkerHosts      = "worker_hosts"
	OutputWorkerIPs        = "worker_ips"
	OutputWorkerIPv6s      = "worker_ipv6s"
	OutputWorkerPrivateIPs = "worker_private_ips"
	OutputSSHKeyName       = "ssh_key_name"
)

// TerraformOutputs is the typed view of terraform output -json values. Raw is
// preserved for compatibility boundaries that still need the original map.
type TerraformOutputs struct {
	Raw        map[string]string
	ToolName   string
	Cloud      cloud.Kind
	AWS        AWSRuntimeOutputs
	Selfhosted SelfhostedRuntimeOutputs
}

type AWSRuntimeOutputs struct {
	SQSQueueURL        string
	ECRRepoURL         string
	ImageTag           string
	DockerImage        string
	S3BucketName       string
	S3Endpoint         string
	S3Region           string
	S3AccessKey        string
	S3SecretKey        string
	S3PathStyle        bool
	ECSClusterName     string
	TaskDefinitionARN  string
	SubnetIDs          []string
	SecurityGroupID    string
	InstanceProfileARN string
	AMIID              string
}

type SelfhostedRuntimeOutputs struct {
	Cloud                  cloud.Kind
	ControllerSecurityMode string
	CredentialScopeVersion string

	NATSURL                        string
	NATSStream                     string
	NATSUser                       string
	NATSPassword                   string
	NATSOperatorUser               string
	NATSOperatorPassword           string
	NATSTLSEnabled                 bool
	NATSMTLSEnabled                bool
	NATSAuthEnabled                bool
	NATSOperatorClientCertPEM      string
	NATSOperatorClientCertNotAfter string
	NATSOperatorClientKeyPEM       string
	NATSWorkerClientCertNotAfter   string

	S3Endpoint          string
	S3Region            string
	S3AccessKey         string
	S3SecretKey         string
	S3OperatorAccessKey string
	S3OperatorSecretKey string
	S3PathStyle         bool
	S3BucketName        string
	MinIOTLSEnabled     bool
	MinIOAuthEnabled    bool

	RegistryURL               string
	RegistryUsername          string
	RegistryPassword          string
	RegistryPublisherUsername string
	RegistryPublisherPassword string
	RegistryTLSEnabled        bool
	RegistryAuthEnabled       bool

	ControllerCAPEM               string
	ControllerCAFingerprintSHA256 string
	ControllerCertNotAfter        string
	ControllerHost                string
	ControllerIP                  string
	ControllerIPv6                string
	GenerationID                  string

	DockerImage string
	QueueURL    string

	WorkerCount      int
	WorkerHosts      []string
	WorkerIPs        []string
	WorkerIPv6s      []string
	WorkerPrivateIPs []string
	SSHKeyName       string
}

// DecodeTerraformOutputs turns the raw terraform output map into typed runtime
// views while preserving the original keys and values for compatibility.
func DecodeTerraformOutputs(kind cloud.Kind, raw map[string]string) TerraformOutputs {
	copied := copyStringMap(raw)
	outputCloud := cloud.Kind(OutputString(copied, OutputCloud)).Canonical()
	if outputCloud == "" {
		outputCloud = kind.Canonical()
	}

	return TerraformOutputs{
		Raw:      copied,
		ToolName: OutputString(copied, OutputToolName),
		Cloud:    outputCloud,
		AWS: AWSRuntimeOutputs{
			SQSQueueURL:        OutputString(copied, OutputSQSQueueURL),
			ECRRepoURL:         OutputString(copied, OutputECRRepoURL),
			ImageTag:           OutputString(copied, OutputImageTag),
			DockerImage:        OutputString(copied, OutputDockerImage),
			S3BucketName:       OutputString(copied, OutputS3BucketName),
			S3Endpoint:         OutputString(copied, OutputS3Endpoint),
			S3Region:           OutputString(copied, OutputS3Region),
			S3AccessKey:        OutputString(copied, OutputS3AccessKey),
			S3SecretKey:        OutputString(copied, OutputS3SecretKey),
			S3PathStyle:        OutputBool(copied, OutputS3PathStyle),
			ECSClusterName:     OutputString(copied, OutputECSClusterName),
			TaskDefinitionARN:  OutputString(copied, OutputTaskDefinitionARN),
			SubnetIDs:          OutputList(copied, OutputSubnetIDs),
			SecurityGroupID:    OutputString(copied, OutputSecurityGroupID),
			InstanceProfileARN: OutputString(copied, OutputInstanceProfile),
			AMIID:              OutputString(copied, OutputAMIID),
		},
		Selfhosted: SelfhostedRuntimeOutputs{
			Cloud:                          outputCloud,
			ControllerSecurityMode:         OutputString(copied, OutputControllerSecurityMode),
			CredentialScopeVersion:         OutputString(copied, OutputCredentialScopeVersion),
			NATSURL:                        OutputString(copied, OutputNATSURL),
			NATSStream:                     OutputString(copied, OutputNATSStream),
			NATSUser:                       OutputString(copied, OutputNATSUser),
			NATSPassword:                   OutputString(copied, OutputNATSPassword),
			NATSOperatorUser:               OutputString(copied, OutputNATSOperatorUser),
			NATSOperatorPassword:           OutputString(copied, OutputNATSOperatorPassword),
			NATSTLSEnabled:                 OutputBool(copied, OutputNATSTLSEnabled),
			NATSMTLSEnabled:                OutputBool(copied, OutputNATSMTLSEnabled),
			NATSAuthEnabled:                OutputBool(copied, OutputNATSAuthEnabled),
			NATSOperatorClientCertPEM:      OutputString(copied, OutputNATSOperatorClientCertPEM),
			NATSOperatorClientCertNotAfter: OutputString(copied, OutputNATSOperatorClientCertNotAfter),
			NATSOperatorClientKeyPEM:       OutputString(copied, OutputNATSOperatorClientKeyPEM),
			NATSWorkerClientCertNotAfter:   OutputString(copied, OutputNATSWorkerClientCertNotAfter),
			S3Endpoint:                     OutputString(copied, OutputS3Endpoint),
			S3Region:                       OutputString(copied, OutputS3Region),
			S3AccessKey:                    OutputString(copied, OutputS3AccessKey),
			S3SecretKey:                    OutputString(copied, OutputS3SecretKey),
			S3OperatorAccessKey:            OutputString(copied, OutputS3OperatorAccessKey),
			S3OperatorSecretKey:            OutputString(copied, OutputS3OperatorSecretKey),
			S3PathStyle:                    OutputBool(copied, OutputS3PathStyle),
			S3BucketName:                   OutputString(copied, OutputS3BucketName),
			MinIOTLSEnabled:                OutputBool(copied, OutputMinIOTLSEnabled),
			MinIOAuthEnabled:               OutputBool(copied, OutputMinIOAuthEnabled),
			RegistryURL:                    OutputString(copied, OutputRegistryURL),
			RegistryUsername:               OutputString(copied, OutputRegistryUsername),
			RegistryPassword:               OutputString(copied, OutputRegistryPassword),
			RegistryPublisherUsername:      OutputString(copied, OutputRegistryPublisherUsername),
			RegistryPublisherPassword:      OutputString(copied, OutputRegistryPublisherPassword),
			RegistryTLSEnabled:             OutputBool(copied, OutputRegistryTLSEnabled),
			RegistryAuthEnabled:            OutputBool(copied, OutputRegistryAuthEnabled),
			ControllerCAPEM:                OutputString(copied, OutputControllerCAPEM),
			ControllerCAFingerprintSHA256:  OutputString(copied, OutputControllerCAFingerprintSHA256),
			ControllerCertNotAfter:         OutputString(copied, OutputControllerCertNotAfter),
			ControllerHost:                 OutputString(copied, OutputControllerHost),
			ControllerIP:                   OutputString(copied, OutputControllerIP),
			ControllerIPv6:                 OutputString(copied, OutputControllerIPv6),
			GenerationID:                   OutputString(copied, OutputGenerationID),
			DockerImage:                    OutputString(copied, OutputDockerImage),
			QueueURL:                       OutputString(copied, OutputSQSQueueURL),
			WorkerCount:                    OutputInt(copied, OutputWorkerCount),
			WorkerHosts:                    OutputList(copied, OutputWorkerHosts),
			WorkerIPs:                      OutputList(copied, OutputWorkerIPs),
			WorkerIPv6s:                    OutputList(copied, OutputWorkerIPv6s),
			WorkerPrivateIPs:               OutputList(copied, OutputWorkerPrivateIPs),
			SSHKeyName:                     OutputString(copied, OutputSSHKeyName),
		},
	}
}

func (o TerraformOutputs) ValidateRequired(kind cloud.Kind) []string {
	return MissingOutputKeys(o.Raw, RequiredOutputKeysForCloud(kind))
}

func (o TerraformOutputs) ToMap() map[string]string {
	return copyStringMap(o.Raw)
}

func (o TerraformOutputs) RedactedMap() map[string]string {
	return RedactOutputs(o.Raw)
}

func MissingOutputKeys(outputs map[string]string, keys []string) []string {
	var missing []string
	for _, key := range keys {
		if outputs[key] == "" {
			missing = append(missing, key)
		}
	}
	return missing
}

func OutputString(outputs map[string]string, key string) string {
	if outputs == nil {
		return ""
	}
	return outputs[key]
}

func OutputBool(outputs map[string]string, key string) bool {
	switch strings.ToLower(strings.TrimSpace(OutputString(outputs, key))) {
	case "true", "1", "yes", "y", "on":
		return true
	default:
		return false
	}
}

func OutputInt(outputs map[string]string, key string) int {
	n, err := strconv.Atoi(strings.TrimSpace(OutputString(outputs, key)))
	if err != nil || n <= 0 {
		return 0
	}
	return n
}

func OutputList(outputs map[string]string, key string) []string {
	value := strings.TrimSpace(OutputString(outputs, key))
	if value == "" {
		return nil
	}
	value = strings.Trim(value, "[]")
	var fields []string
	if strings.Contains(value, ",") {
		fields = strings.Split(value, ",")
	} else {
		fields = strings.Fields(value)
	}
	result := make([]string, 0, len(fields))
	for _, field := range fields {
		field = strings.TrimSpace(field)
		if field != "" {
			result = append(result, field)
		}
	}
	return result
}

func copyStringMap(src map[string]string) map[string]string {
	if src == nil {
		return map[string]string{}
	}
	dst := make(map[string]string, len(src))
	for k, v := range src {
		dst[k] = v
	}
	return dst
}

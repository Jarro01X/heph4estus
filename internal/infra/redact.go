package infra

import "strings"

// sensitivePatterns is the list of substrings that mark a Terraform output key
// as sensitive. Matching is case-insensitive against the key name.
var sensitivePatterns = []string{
	"secret",
	"password",
	"token",
	"credential",
	"access_key",
	"nats_url", // contains embedded auth credentials
	"controller_ca_pem",
}

const redactedPlaceholder = "***"

// IsSensitiveOutput returns true when the output key name matches any of the
// known sensitive patterns. Use this to decide whether a value should be
// redacted before logging or displaying it to the operator.
func IsSensitiveOutput(key string) bool {
	lower := strings.ToLower(key)
	for _, p := range sensitivePatterns {
		if strings.Contains(lower, p) {
			return true
		}
	}
	return false
}

// RedactOutputValue returns the original value when the key is safe, or a
// redaction placeholder when it matches a sensitive pattern.
func RedactOutputValue(key, value string) string {
	if IsSensitiveOutput(key) {
		return redactedPlaceholder
	}
	return value
}

// RedactOutputs returns a copy of the outputs map with sensitive values masked.
func RedactOutputs(outputs map[string]string) map[string]string {
	redacted := make(map[string]string, len(outputs))
	for k, v := range outputs {
		redacted[k] = RedactOutputValue(k, v)
	}
	return redacted
}

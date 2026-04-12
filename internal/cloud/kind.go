package cloud

import (
	"fmt"
	"strings"
)

// Kind identifies a cloud/provider family. Use ParseKind to construct from
// user input rather than comparing raw strings throughout the codebase.
type Kind string

const (
	// KindAWS selects the AWS provider (SQS + S3 + ECS/EC2).
	KindAWS Kind = "aws"
	// KindSelfhosted selects the selfhosted provider (NATS JetStream + S3-compatible).
	// The full data plane lands in PR 6.1 Track 1/2; PR 6.1 Track 0 only adds the
	// identity and config plumbing.
	KindSelfhosted Kind = "selfhosted"
)

// DefaultKind is the cloud used when nothing else is specified.
const DefaultKind = KindAWS

// SupportedKinds returns the canonical list of cloud kinds.
func SupportedKinds() []Kind {
	return []Kind{KindAWS, KindSelfhosted}
}

// String returns the canonical string form of the kind.
func (k Kind) String() string { return string(k) }

// ParseKind validates and normalizes a cloud kind string. An empty input
// returns DefaultKind so callers can pass through unset config values.
func ParseKind(s string) (Kind, error) {
	trimmed := strings.TrimSpace(strings.ToLower(s))
	if trimmed == "" {
		return DefaultKind, nil
	}
	for _, k := range SupportedKinds() {
		if trimmed == string(k) {
			return k, nil
		}
	}
	return "", fmt.Errorf("unsupported cloud %q (expected one of: %s)", s, joinKinds(SupportedKinds()))
}

func joinKinds(ks []Kind) string {
	parts := make([]string, len(ks))
	for i, k := range ks {
		parts[i] = string(k)
	}
	return strings.Join(parts, ", ")
}

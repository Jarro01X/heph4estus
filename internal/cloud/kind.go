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
	// KindManual selects the manual/operator-managed VPS path.
	KindManual Kind = "manual"
	// KindSelfhosted is a deprecated alias for KindManual, kept for backwards
	// compatibility with older configs and CLI usage.
	KindSelfhosted Kind = "manual"
	// VPS/provider selectors. Today they share the same selfhosted runtime
	// family; later PRs can attach provider-aware fleet managers to them.
	KindHetzner  Kind = "hetzner"
	KindLinode   Kind = "linode"
	KindScaleway Kind = "scaleway"
	KindVultr    Kind = "vultr"
)

// DefaultKind is the cloud used when nothing else is specified.
const DefaultKind = KindAWS

// SupportedKinds returns the canonical list of cloud kinds.
func SupportedKinds() []Kind {
	return []Kind{KindAWS, KindManual, KindHetzner, KindLinode, KindScaleway, KindVultr}
}

// String returns the canonical string form of the kind.
func (k Kind) String() string { return string(k) }

// Canonical returns the canonical display/storage form of the kind.
func (k Kind) Canonical() Kind {
	switch strings.ToLower(strings.TrimSpace(string(k))) {
	case "selfhosted", "manual":
		return KindManual
	case "aws":
		return KindAWS
	case "hetzner":
		return KindHetzner
	case "linode":
		return KindLinode
	case "scaleway":
		return KindScaleway
	case "vultr":
		return KindVultr
	default:
		return k
	}
}

// IsSelfhostedFamily reports whether the kind belongs to the VPS/selfhosted
// runtime family.
func (k Kind) IsSelfhostedFamily() bool {
	switch k.Canonical() {
	case KindManual, KindHetzner, KindLinode, KindScaleway, KindVultr:
		return true
	default:
		return false
	}
}

// RuntimeFamily collapses provider-specific selectors onto the shared
// execution family used by the implementation.
//
// Hetzner keeps its own kind so the factory can route it to the fleet-manager
// path instead of the manual SSH path.  All selfhosted-family providers share
// the same queue/storage runtime but Hetzner has provider-native deploy.
func (k Kind) RuntimeFamily() Kind {
	switch k.Canonical() {
	case KindHetzner:
		return KindHetzner
	case KindManual, KindLinode, KindScaleway, KindVultr:
		return KindManual
	default:
		return KindAWS
	}
}

// IsProviderNative reports whether the kind has provider-native deploy
// support (Terraform + fleet manager) as opposed to manual/operator-managed.
func (k Kind) IsProviderNative() bool {
	switch k.Canonical() {
	case KindHetzner:
		return true
	default:
		return false
	}
}

// SupportedKindsText returns the canonical user-facing list for help text.
func SupportedKindsText() string {
	return joinKinds(SupportedKinds())
}

// ParseKind validates and normalizes a cloud kind string. An empty input
// returns DefaultKind so callers can pass through unset config values.
func ParseKind(s string) (Kind, error) {
	trimmed := strings.TrimSpace(strings.ToLower(s))
	if trimmed == "" {
		return DefaultKind, nil
	}
	if trimmed == "selfhosted" {
		return KindManual, nil
	}
	for _, k := range SupportedKinds() {
		if trimmed == string(k) {
			return k, nil
		}
	}
	return "", fmt.Errorf("unsupported cloud %q (expected one of: %s)", s, SupportedKindsText())
}

// ValidateComputeMode checks whether the given compute mode is allowed for
// the selected cloud provider. This is the single authority for cloud-specific
// mode policy so callers (CLI and TUI) do not duplicate the check.
//
// Policy:
//   - AWS: auto, fargate, spot (and empty, which resolves to auto).
//   - Selfhosted: auto only (fargate and spot are AWS-specific concepts).
func ValidateComputeMode(kind Kind, mode string) error {
	switch {
	case kind.IsSelfhostedFamily():
		if mode != "" && mode != "auto" {
			return fmt.Errorf("provider %q only supports compute-mode \"auto\", got %q (fargate and spot are AWS-specific)", kind.Canonical(), mode)
		}
	default: // AWS
		switch mode {
		case "", "auto", "fargate", "spot":
			// all valid
		default:
			return fmt.Errorf("compute-mode must be auto, fargate, or spot, got %q", mode)
		}
	}
	return nil
}

func joinKinds(ks []Kind) string {
	parts := make([]string, len(ks))
	for i, k := range ks {
		parts[i] = string(k)
	}
	return strings.Join(parts, ", ")
}

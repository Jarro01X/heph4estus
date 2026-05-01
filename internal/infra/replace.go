package infra

import (
	"fmt"

	"heph4estus/internal/cloud"
)

// WorkerResourceAddress returns the Terraform resource address for a worker VM
// index on the given provider-native cloud.
func WorkerResourceAddress(kind cloud.Kind, index int) (string, error) {
	switch kind.Canonical() {
	case cloud.KindHetzner:
		return fmt.Sprintf("hcloud_server.worker[%d]", index), nil
	case cloud.KindLinode:
		return fmt.Sprintf("linode_instance.worker[%d]", index), nil
	case cloud.KindVultr:
		return fmt.Sprintf("vultr_instance.worker[%d]", index), nil
	default:
		return "", fmt.Errorf("worker replacement is not supported for %q", kind.Canonical())
	}
}

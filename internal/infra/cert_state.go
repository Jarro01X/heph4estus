package infra

import (
	"encoding/json"
	"fmt"
	"strings"
)

type terraformShowDocument struct {
	Values terraformShowValues `json:"values"`
}

type terraformShowValues struct {
	RootModule terraformStateModule `json:"root_module"`
}

type terraformStateModule struct {
	Address      string                   `json:"address"`
	Resources    []terraformStateResource `json:"resources"`
	ChildModules []terraformStateModule   `json:"child_modules"`
}

type terraformStateResource struct {
	Address string                 `json:"address"`
	Type    string                 `json:"type"`
	Name    string                 `json:"name"`
	Values  map[string]interface{} `json:"values"`
}

// ControllerCAPrivateKeyFromTerraformShow extracts the controller CA private
// key from terraform show -json output. The key is intentionally not exposed as
// a Terraform output, but controller server cert rotation needs it to sign a new
// leaf certificate without changing the CA that workers already trust.
func ControllerCAPrivateKeyFromTerraformShow(data []byte) (string, error) {
	var doc terraformShowDocument
	if err := json.Unmarshal(data, &doc); err != nil {
		return "", fmt.Errorf("parsing terraform show JSON: %w", err)
	}
	key := controllerCAPrivateKeyFromModule(doc.Values.RootModule)
	if strings.TrimSpace(key) == "" {
		return "", fmt.Errorf("controller CA private key was not found in Terraform state")
	}
	return normalizePEM(key), nil
}

func controllerCAPrivateKeyFromModule(module terraformStateModule) string {
	for _, resource := range module.Resources {
		if !isControllerCAPrivateKeyResource(resource) {
			continue
		}
		if value, ok := resource.Values["private_key_pem"].(string); ok {
			return value
		}
	}
	for _, child := range module.ChildModules {
		if value := controllerCAPrivateKeyFromModule(child); value != "" {
			return value
		}
	}
	return ""
}

func isControllerCAPrivateKeyResource(resource terraformStateResource) bool {
	if resource.Type == "tls_private_key" && resource.Name == "controller_ca" {
		return true
	}
	return strings.HasSuffix(resource.Address, ".tls_private_key.controller_ca") || resource.Address == "tls_private_key.controller_ca"
}

func normalizePEM(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	return value + "\n"
}

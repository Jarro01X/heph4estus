package infra

import (
	"strings"
	"testing"
)

func TestControllerCAPrivateKeyFromTerraformShow(t *testing.T) {
	state := `{
  "values": {
    "root_module": {
      "child_modules": [
        {
          "address": "module.controller",
          "resources": [
            {
              "address": "module.controller.tls_private_key.controller_ca",
              "type": "tls_private_key",
              "name": "controller_ca",
              "values": {
                "private_key_pem": "-----BEGIN EC PRIVATE KEY-----\nabc\n-----END EC PRIVATE KEY-----"
              }
            }
          ]
        }
      ]
    }
  }
}`
	key, err := ControllerCAPrivateKeyFromTerraformShow([]byte(state))
	if err != nil {
		t.Fatalf("ControllerCAPrivateKeyFromTerraformShow: %v", err)
	}
	if !strings.HasSuffix(key, "\n") {
		t.Fatalf("expected normalized trailing newline: %q", key)
	}
	if !strings.Contains(key, "BEGIN EC PRIVATE KEY") {
		t.Fatalf("unexpected key: %q", key)
	}
}

func TestControllerCAPrivateKeyFromTerraformShowRejectsMissingKey(t *testing.T) {
	_, err := ControllerCAPrivateKeyFromTerraformShow([]byte(`{"values":{"root_module":{"resources":[]}}}`))
	if err == nil {
		t.Fatal("expected missing key error")
	}
	if !strings.Contains(err.Error(), "not found") {
		t.Fatalf("unexpected error: %v", err)
	}
}

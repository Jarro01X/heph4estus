package main

import (
	"fmt"
	"strings"
)

func awsImageTagFromOutputs(outputs map[string]string) (string, error) {
	tag := strings.TrimSpace(outputs["image_tag"])
	if tag == "" {
		return "", fmt.Errorf("terraform outputs missing image_tag")
	}
	return tag, nil
}

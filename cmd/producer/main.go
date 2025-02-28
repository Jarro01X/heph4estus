package main

import (
	"context"
	"encoding/json"
	"flag"
	"log"
	"os"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/sfn"
)

type ScanTarget struct {
	Target  string `json:"target"`
	Options string `json:"options"`
}

type StepFunctionInput struct {
	Targets []ScanTarget `json:"targets"`
}

func main() {
	// Command line flags for input file and default options
	inputFile := flag.String("file", "", "Path to file containing targets")
	defaultOptions := flag.String("default-options", "-sS", "Default Nmap options")
	flag.Parse()

	log.Println("Scanner application starting...")

	if *inputFile == "" {
		log.Fatal("Please provide an input file with -file flag")
	}

	// Read targets from file
	content, err := os.ReadFile(*inputFile)
	if err != nil {
		log.Fatalf("Error reading file: %v", err)
	}

	// Parse targets and prepare Step Functions input
	lines := strings.Split(strings.TrimSpace(string(content)), "\n")
	targets := make([]ScanTarget, 0, len(lines))

	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		parts := strings.Fields(line)
		target := ScanTarget{
			Target:  parts[0],
			Options: *defaultOptions,
		}
		if len(parts) > 1 {
			target.Options = strings.Join(parts[1:], " ")
		}
		targets = append(targets, target)
	}

	// Initialize AWS client
	cfg, err := config.LoadDefaultConfig(context.TODO())
	if err != nil {
		log.Fatalf("Unable to load SDK config: %v", err)
	}

	client := sfn.NewFromConfig(cfg)

	// Start Step Functions execution
	input := StepFunctionInput{Targets: targets}
	inputJSON, _ := json.Marshal(input)

	_, err = client.StartExecution(context.TODO(), &sfn.StartExecutionInput{
		StateMachineArn: aws.String(os.Getenv("STATE_MACHINE_ARN")),
		Input:           aws.String(string(inputJSON)),
	})
	if err != nil {
		log.Fatalf("Error starting execution: %v", err)
	}

	log.Printf("Successfully started scan for %d targets", len(targets))
}

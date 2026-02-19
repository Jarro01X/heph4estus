package main

import (
	"context"
	"encoding/json"
	"flag"
	"heph4estus/internal/cloud/aws"
	appconfig "heph4estus/internal/config"
	"heph4estus/internal/logger"
	"heph4estus/internal/tools/nmap"
	"os"

	"github.com/aws/aws-sdk-go-v2/config"
)

func main() {
	log := logger.NewSimpleLogger()
	log.Info("Scanner producer application starting...")

	// Command line flags for input file and default options
	inputFile := flag.String("file", "", "Path to file containing targets")
	defaultOptions := flag.String("default-options", "-sS", "Default Nmap options")
	flag.Parse()

	if *inputFile == "" {
		log.Fatal("Please provide an input file with -file flag")
	}

	// Load configuration
	cfg, err := appconfig.NewProducerConfig()
	if err != nil {
		log.Fatal("Failed to load configuration: %v", err)
	}

	log.Info("Using state machine ARN: %s", cfg.StateMachineARN)
	log.Info("Using input file: %s", *inputFile)
	log.Info("Using default options: %s", *defaultOptions)

	// Read targets from file
	content, err := os.ReadFile(*inputFile)
	if err != nil {
		log.Fatal("Error reading file: %v", err)
	}

	// Initialize services
	log.Info("Initializing AWS client...")
	awsCfg, err := config.LoadDefaultConfig(context.TODO())
	if err != nil {
		log.Fatal("Unable to load SDK config: %v", err)
	}

	sfnClient := aws.NewSFNClient(awsCfg, log)
	scannerSvc := nmap.NewScanner(log)

	// Parse targets and prepare Step Functions input
	targets := scannerSvc.ParseTargets(string(content), *defaultOptions)
	log.Info("Parsed %d targets from file", len(targets))

	// Start Step Functions execution
	input := nmap.StepFunctionInput{Targets: targets}
	inputJSON, err := json.Marshal(input)
	if err != nil {
		log.Fatal("Error marshaling input: %v", err)
	}

	_, err = sfnClient.StartExecution(context.TODO(), cfg.StateMachineARN, string(inputJSON))
	if err != nil {
		log.Fatal("Error starting execution: %v", err)
	}

	log.Info("Successfully started scan for %d targets", len(targets))
}

package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"

	"heph4estus/internal/cloud/aws"
	appconfig "heph4estus/internal/config"
	"heph4estus/internal/logger"
	"heph4estus/internal/tools/nmap"

	awscfg "github.com/aws/aws-sdk-go-v2/config"
)

func runNmap(args []string, log logger.Logger) error {
	fs := flag.NewFlagSet("nmap", flag.ContinueOnError)
	inputFile := fs.String("file", "", "Path to file containing targets")
	defaultOptions := fs.String("default-options", "-sS", "Default Nmap options")
	format := fs.String("format", "text", "Output format: json or text")

	if err := fs.Parse(args); err != nil {
		return err
	}

	if *inputFile == "" {
		return fmt.Errorf("--file flag is required")
	}

	_ = *format // reserved for future use

	cfg, err := appconfig.NewProducerConfig()
	if err != nil {
		return fmt.Errorf("failed to load configuration: %w", err)
	}

	log.Info("Using state machine ARN: %s", cfg.StateMachineARN)
	log.Info("Using input file: %s", *inputFile)
	log.Info("Using default options: %s", *defaultOptions)

	content, err := os.ReadFile(*inputFile)
	if err != nil {
		return fmt.Errorf("error reading file: %w", err)
	}

	log.Info("Initializing AWS client...")
	awsConfig, err := awscfg.LoadDefaultConfig(context.TODO())
	if err != nil {
		return fmt.Errorf("unable to load SDK config: %w", err)
	}

	sfnClient := aws.NewSFNClient(awsConfig, log)
	scannerSvc := nmap.NewScanner(log)

	targets := scannerSvc.ParseTargets(string(content), *defaultOptions)
	log.Info("Parsed %d targets from file", len(targets))

	input := nmap.StepFunctionInput{Targets: targets}
	inputJSON, err := json.Marshal(input)
	if err != nil {
		return fmt.Errorf("error marshaling input: %w", err)
	}

	_, err = sfnClient.StartExecution(context.TODO(), cfg.StateMachineARN, string(inputJSON))
	if err != nil {
		return fmt.Errorf("error starting execution: %w", err)
	}

	log.Info("Successfully started scan for %d targets", len(targets))
	return nil
}

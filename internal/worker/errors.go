package worker

import "strings"

// ErrorKind classifies scan errors for retry decisions.
type ErrorKind int

const (
	// ErrorTransient indicates a retryable failure (network flap, DNS timeout).
	// The SQS message should NOT be deleted — visibility timeout triggers retry.
	ErrorTransient ErrorKind = iota

	// ErrorPermanent indicates an unrecoverable failure (bad options, invalid target).
	// The error result should be uploaded and the SQS message deleted.
	ErrorPermanent
)

// transientPatterns are substrings that indicate a retryable failure.
// These are checked BEFORE permanent patterns so that ambiguous cases
// (e.g., DNS timeout vs permanent DNS failure) default to retry.
var transientPatterns = []string{
	"scan timed out",
	"command timed out",
	"Temporary failure in name resolution",
	"sendto: Network is unreachable",
	"Host seems down",
	"QUITTING!",
	"connection timed out",
	"No route to host",
	"i/o timeout",
	"connection refused",
}

// ClassifyError examines command output and error text to determine retry behavior.
// Transient patterns are checked first. If no transient pattern matches, the
// error is classified as permanent. Unknown errors default to permanent to
// avoid infinite retry loops — SQS maxReceiveCount + DLQ handles the case
// where a transient error exhausts retries.
func ClassifyError(output, errText string) ErrorKind {
	combined := output + " " + errText
	lower := strings.ToLower(combined)

	for _, pattern := range transientPatterns {
		if strings.Contains(lower, strings.ToLower(pattern)) {
			return ErrorTransient
		}
	}

	return ErrorPermanent
}

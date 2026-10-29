package kubernetes

import (
	"time"

	log "github.com/sirupsen/logrus"
)

const (
	// Startup calls to the Kubernetes API are retried because a single
	// transient failure there — a TLS handshake timeout during node churn,
	// say — would otherwise take the pod down before it has served anything.
	k8sStartupAttempts  = 5
	k8sStartupBaseDelay = time.Second
)

// retry runs op until it succeeds or attempts is exhausted, doubling delay
// between tries. It returns the error from the final attempt.
func retry(attempts int, delay time.Duration, op func() error) error {
	var err error
	for attempt := 1; attempt <= attempts; attempt++ {
		if err = op(); err == nil {
			return nil
		}
		if attempt == attempts {
			break
		}
		log.WithError(err).Warnf("Kubernetes API call failed (attempt %d/%d), retrying in %s",
			attempt, attempts, delay)
		time.Sleep(delay)
		delay *= 2
	}
	return err
}

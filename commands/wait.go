package commands

import (
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/ariaservice/arianet-cli/internal/api"
)

const (
	defaultWaitTimeout = 15 * time.Minute
	// Transient errors tolerated in a row before the wait gives up.
	maxPollErrors = 6
)

var (
	pollInterval = 5 * time.Second
	// A restart or reinstall can still read "active" for a moment after the
	// API accepted it; give the platform this long to show a transition
	// before an unchanged "active" is believed.
	transitionGrace = 20 * time.Second
)

// waitOutcome is how a wait ended.
type waitOutcome int

const (
	waitDone waitOutcome = iota
	waitFailed
	waitTimedOut
	waitAborted
)

// waitSpec describes what "finished" means for one operation.
type waitSpec struct {
	label string
	// needsTransition makes an unchanged state count only after a transition
	// was seen or transitionGrace has passed.
	needsTransition bool
	// goneIsDone treats a 404 as success (deleted servers disappear).
	goneIsDone bool
	// settled reports whether the server reached the target state.
	settled func(st *api.ServiceStatus) bool
	// failed reports a terminal state that will never become the target.
	failed func(st *api.ServiceStatus) bool
}

func isRunning(st *api.ServiceStatus) bool {
	ps := strings.ToUpper(st.ProviderStatus())
	return ps == "" || ps == "RUNNING"
}

func isStopped(st *api.ServiceStatus) bool {
	switch strings.ToUpper(st.ProviderStatus()) {
	case "STOPPED", "SHUTOFF", "SHUTDOWN":
		return st.Status == "active"
	}
	return false
}

func failedStatus(st *api.ServiceStatus) bool {
	switch st.Status {
	case "failed", "terminated", "abuse_suspended":
		return true
	}
	return false
}

func waitProvisioned() waitSpec {
	return waitSpec{
		label:   "Provisioning",
		settled: func(st *api.ServiceStatus) bool { return st.Status == "active" },
		failed:  failedStatus,
	}
}

func waitRestarted() waitSpec {
	return waitSpec{
		label:           "Restart",
		needsTransition: true,
		settled:         func(st *api.ServiceStatus) bool { return st.Status == "active" && isRunning(st) },
		failed:          failedStatus,
	}
}

func waitPoweredOn() waitSpec {
	return waitSpec{
		label:   "Power on",
		settled: func(st *api.ServiceStatus) bool { return st.Status == "active" && isRunning(st) },
		failed:  failedStatus,
	}
}

func waitPoweredOff() waitSpec {
	return waitSpec{
		label:   "Power off",
		settled: isStopped,
		failed:  failedStatus,
	}
}

func waitReinstalled() waitSpec {
	return waitSpec{
		label:           "Reinstall",
		needsTransition: true,
		settled:         func(st *api.ServiceStatus) bool { return st.Status == "active" && isRunning(st) },
		failed:          failedStatus,
	}
}

func waitDeleted() waitSpec {
	return waitSpec{
		label:      "Deletion",
		goneIsDone: true,
		settled:    func(st *api.ServiceStatus) bool { return st.Status == "terminated" },
		// A terminated server is the goal here, so only "failed" is a failure.
		failed: func(st *api.ServiceStatus) bool { return st.Status == "failed" },
	}
}

// waitForServer polls until the operation finishes, fails or the timeout
// passes. Progress goes to stderr so stdout stays clean for --output json.
func waitForServer(client *api.Client, id int, spec waitSpec, timeout time.Duration) waitOutcome {
	if timeout <= 0 {
		timeout = defaultWaitTimeout
	}

	start := time.Now()
	deadline := start.Add(timeout)
	lastLine := ""
	lastPrint := time.Time{}
	sawTransition := false
	errCount := 0

	say := func(line string) {
		// Repeat an unchanged line only now and then, as a sign of life.
		if line == lastLine && time.Since(lastPrint) < 30*time.Second {
			return
		}
		lastLine, lastPrint = line, time.Now()
		fmt.Fprintf(os.Stderr, "  [%s] %s\n", fmtElapsed(time.Since(start)), line)
	}

	fmt.Fprintf(os.Stderr, "\n  Waiting for server #%d (Ctrl+C stops waiting; the operation continues)\n", id)

	for {
		st, err := client.GetServiceStatus(id)
		switch {
		case err != nil && spec.goneIsDone && api.IsNotFound(err):
			say("server no longer exists")
			return waitDone
		case err != nil && !pollErrorIsTransient(err):
			fmt.Fprintf(os.Stderr, "  Stopped waiting: %s\n", err.Error())
			return waitAborted
		case err != nil:
			errCount++
			if errCount >= maxPollErrors {
				fmt.Fprintf(os.Stderr, "  Stopped waiting: %s\n", err.Error())
				return waitAborted
			}
			say("status check failed, retrying: " + err.Error())
		default:
			errCount = 0
			say(describeStatus(st))

			if spec.failed(st) {
				return waitFailed
			}
			if st.Status != "active" || !isRunning(st) {
				sawTransition = true
			}
			if spec.settled(st) && (!spec.needsTransition || sawTransition || time.Since(start) >= transitionGrace) {
				return waitDone
			}
		}

		if !time.Now().Add(pollInterval).Before(deadline) {
			return waitTimedOut
		}
		time.Sleep(pollInterval)
	}
}

// pollErrorIsTransient reports whether a failed status check is worth
// repeating (network trouble, a busy gateway, a rate limit).
func pollErrorIsTransient(err error) bool {
	apiErr, ok := err.(*api.APIError)
	if !ok {
		return true
	}
	return apiErr.Status >= 500 || apiErr.Status == 429
}

func describeStatus(st *api.ServiceStatus) string {
	line := "status: " + st.Status
	if ps := st.ProviderStatus(); ps != "" {
		line += " (provider: " + strings.ToLower(ps) + ")"
	}
	return line
}

func fmtElapsed(d time.Duration) string {
	s := int(d.Seconds())
	return fmt.Sprintf("%d:%02d", s/60, s%60)
}

// finishWait prints the result of a wait and exits non-zero when the
// operation did not end well, so scripts can rely on the exit code.
func finishWait(id int, spec waitSpec, outcome waitOutcome, timeout time.Duration) {
	switch outcome {
	case waitDone:
		fmt.Fprintf(os.Stderr, "\n  %s finished for server #%d.\n\n", spec.label, id)
	case waitFailed:
		fmt.Fprintf(os.Stderr, "\n  %s failed for server #%d. Details: arianet server actions %d\n\n", spec.label, id, id)
		os.Exit(1)
	case waitTimedOut:
		fmt.Fprintf(os.Stderr, "\n  %s is still in progress after %s. Check: arianet server status %d\n\n", spec.label, timeout, id)
		os.Exit(1)
	case waitAborted:
		fmt.Fprintf(os.Stderr, "  Check manually: arianet server status %d\n\n", id)
		os.Exit(1)
	}
}

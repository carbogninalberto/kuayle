package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func readStatus(t *testing.T, u *updater) updateResponse {
	t.Helper()
	rec := httptest.NewRecorder()
	u.handleStatus(rec, httptest.NewRequest(http.MethodGet, "/status", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status code = %d", rec.Code)
	}
	var status updateResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &status); err != nil {
		t.Fatal(err)
	}
	return status
}

func startedUpdater() *updater {
	u := &updater{message: "Idle", state: stateIdle}
	u.running, u.state, u.output = true, stateRunning, &outputTail{}
	return u
}

func TestFailedUpdateReportsReasonAndOutput(t *testing.T) {
	u := startedUpdater()
	u.runUpdate(func() error {
		fmt.Fprintln(u.output, "Pulling images...")
		fmt.Fprintln(u.output, "Release update failed: manifest unknown")
		return errors.New("exit status 1")
	})

	status := readStatus(t, u)
	if status.Running || status.State != stateFailed || status.FinishedAt == nil {
		t.Fatalf("unexpected status %+v", status)
	}
	if status.Error != "Release update failed: manifest unknown" {
		t.Fatalf("error = %q", status.Error)
	}
	if !strings.Contains(status.Output, "Pulling images...") {
		t.Fatalf("output = %q", status.Output)
	}
}

func TestFailedUpdateWithoutOutputReportsExitError(t *testing.T) {
	u := startedUpdater()
	u.runUpdate(func() error { return errors.New("exit status 2") })

	if status := readStatus(t, u); status.Error != "Update script failed: exit status 2" {
		t.Fatalf("error = %q", status.Error)
	}
}

func TestSuccessfulUpdateOmitsOutput(t *testing.T) {
	u := startedUpdater()
	u.runUpdate(func() error {
		fmt.Fprintln(u.output, "done")
		return nil
	})

	status := readStatus(t, u)
	if status.State != stateSucceeded || status.Error != "" || status.Output != "" {
		t.Fatalf("unexpected status %+v", status)
	}
}

func TestOutputTailKeepsMostRecentBytes(t *testing.T) {
	tail := &outputTail{}
	fmt.Fprint(tail, strings.Repeat("a", maxOutputBytes))
	fmt.Fprint(tail, "\nlast line\n")

	if got := tail.String(); len(got) != maxOutputBytes || !strings.HasSuffix(got, "last line\n") {
		t.Fatalf("tail length %d suffix %q", len(got), got[len(got)-12:])
	}
	if tail.lastLine() != "last line" {
		t.Fatalf("last line = %q", tail.lastLine())
	}
}

package machine

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/kuayle/kuayle-backend/internal/domain"
	log "github.com/sirupsen/logrus"
)

const (
	// environmentCheckMaxAge is how long a developer container's cached
	// environment check is reused before the port scan probes it again.
	environmentCheckMaxAge = 60 * time.Second
	// packageIndexProbeTimeout bounds one probe of the developer container.
	packageIndexProbeTimeout = 3 * time.Second
	// packageIndexRefreshTimeout bounds "sudo apt-get update".
	packageIndexRefreshTimeout = 120 * time.Second
	// A refresh records the last refreshOutputLines lines of its output, at
	// most refreshOutputBytes of them.
	refreshOutputLines = 40
	refreshOutputBytes = 8 * 1024
)

// PackageIndexProbe is what a probe found in /var/lib/apt/lists: whether a
// Packages list exists and, when it does, the newest one's modification time.
type PackageIndexProbe struct {
	Present bool
	Mtime   time.Time
}

func parsePackageIndexProbe(output string) (PackageIndexProbe, error) {
	fields := strings.Fields(output)
	switch {
	case len(fields) == 1 && fields[0] == "absent":
		return PackageIndexProbe{}, nil
	case len(fields) == 2 && fields[0] == "present":
		seconds, err := strconv.ParseInt(fields[1], 10, 64)
		if err != nil || seconds <= 0 {
			return PackageIndexProbe{}, fmt.Errorf("unreadable package index time %q", fields[1])
		}
		return PackageIndexProbe{Present: true, Mtime: time.Unix(seconds, 0).UTC()}, nil
	default:
		return PackageIndexProbe{}, fmt.Errorf("unexpected package index probe output")
	}
}

// outputTail returns the last lines of output, at most maxBytes of them, cut
// on a line (or at least a character) boundary.
func outputTail(output string, lines, maxBytes int) string {
	output = strings.TrimRight(strings.ReplaceAll(output, "\r\n", "\n"), "\n")
	all := strings.Split(output, "\n")
	if len(all) > lines {
		all = all[len(all)-lines:]
	}
	tail := strings.Join(all, "\n")
	if len(tail) > maxBytes {
		tail = tail[len(tail)-maxBytes:]
		if newline := strings.IndexByte(tail, '\n'); newline >= 0 && newline < len(tail)-1 {
			tail = tail[newline+1:]
		}
		for len(tail) > 0 && !utf8.RuneStart(tail[0]) {
			tail = tail[1:]
		}
	}
	return strings.ToValidUTF8(tail, "")
}

// syncEnvironmentStatus probes the running developer container's package
// index when its cached check is missing, older than environmentCheckMaxAge
// or describes another container. It never runs anything in a container
// that is not running, and never changes machine or service health.
func (m *Manager) syncEnvironmentStatus(ctx context.Context, machine *domain.DevMachine, services []domain.DevMachineService) error {
	container := runningDeveloperContainerID(services)
	if machine.Status != domain.DevMachineStatusRunning || container == "" {
		return nil
	}
	check, err := m.store.GetEnvironmentCheck(ctx, machine.ID)
	if err != nil {
		return err
	}
	if check != nil && check.ContainerID == container && time.Since(check.CheckedAt) < environmentCheckMaxAge {
		return nil
	}
	return m.probePackageIndex(ctx, machine, services, container)
}

func (m *Manager) probePackageIndex(ctx context.Context, machine *domain.DevMachine, services []domain.DevMachineService, container string) error {
	probeCtx, cancel := context.WithTimeout(ctx, packageIndexProbeTimeout)
	probe, err := m.runtime.ProbePackageIndex(probeCtx, services)
	cancel()
	state, mtime := domain.PackageIndexAbsent, (*time.Time)(nil)
	switch {
	case err != nil:
		// A probe that fails or times out tells nothing about the index.
		if ctx.Err() != nil {
			return ctx.Err()
		}
		state = domain.PackageIndexUnknown
		log.WithFields(log.Fields{"machine_id": machine.ID, "event_type": "machine.environment_check"}).WithError(err).Debug("package index probe failed")
	case probe.Present:
		state, mtime = domain.PackageIndexPresent, &probe.Mtime
	}
	return m.store.UpsertPackageIndexProbe(ctx, machine.ID, machine.WorkspaceID, container, state, mtime)
}

// refreshPackageIndex runs the owner's requested "sudo apt-get update" once.
// apt failing (an unreachable mirror through the egress proxy, say) is a
// result shown to the owner, not an operation failure.
func (m *Manager) refreshPackageIndex(ctx context.Context, machine *domain.DevMachine, services []domain.DevMachineService) error {
	container := runningDeveloperContainerID(services)
	if machine.Status != domain.DevMachineStatusRunning || container == "" {
		return &terminalOperationError{code: "machine_not_running", message: "the machine is not running"}
	}
	if !machine.SudoEnabled {
		message := "sudo is not enabled for this machine"
		exitCode := -1
		_ = m.store.SetPackageIndexRefresh(ctx, machine.ID, container, domain.PackageIndexRefreshFailed, &exitCode, &message)
		return &terminalOperationError{code: "sudo_disabled", message: message}
	}
	if err := m.store.SetPackageIndexRefresh(ctx, machine.ID, container, domain.PackageIndexRefreshRunning, nil, nil); err != nil {
		return err
	}
	refreshCtx, cancel := context.WithTimeout(ctx, packageIndexRefreshTimeout)
	exitCode, output, err := m.runtime.RefreshPackageIndex(refreshCtx, services)
	cancel()
	state := domain.PackageIndexRefreshSucceeded
	if err != nil {
		exitCode, state = -1, domain.PackageIndexRefreshFailed
		output = strings.TrimSpace(output + "\n" + safeError(err))
	} else if exitCode != 0 {
		state = domain.PackageIndexRefreshFailed
	}
	tail := outputTail(output, refreshOutputLines, refreshOutputBytes)
	if recordErr := m.store.SetPackageIndexRefresh(ctx, machine.ID, container, state, &exitCode, &tail); recordErr != nil {
		return errors.Join(err, recordErr)
	}
	if err != nil {
		return &terminalOperationError{code: "package_index_refresh_failed", message: safeError(err)}
	}
	// Show the index the refresh left at once, not at the next scan.
	return m.probePackageIndex(ctx, machine, services, container)
}

// environmentEnded forgets the machine's environment check: it described a
// developer container that stopped or is gone.
func (m *Manager) environmentEnded(ctx context.Context, machine *domain.DevMachine) {
	if err := m.store.DeleteEnvironmentCheck(ctx, machine.ID); err != nil {
		log.WithFields(log.Fields{"machine_id": machine.ID, "event_type": "machine.environment_check"}).WithError(err).Warn("failed to forget environment check")
	}
}

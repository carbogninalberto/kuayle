package domain

import (
	"fmt"
	"strconv"
	"strings"
)

// Resource limit kinds recorded on a machine when one of its containers hits
// a limit Kuayle enforces.
const (
	DevMachineResourceLimitMemory = "memory"
	DevMachineResourceLimitDisk   = "disk"
)

const (
	memoryLimitReasonPrefix = "Out of memory: "
	diskLimitReasonPrefix   = "Disk full: "
)

// MemoryLimitMessage explains an out-of-memory kill of one machine container.
// limitBytes is the container's cgroup memory limit; zero omits it. Task
// attempts also point at the workspace task machine size.
func MemoryLimitMessage(service string, limitBytes int64, taskMachine bool) string {
	limit := "its memory limit"
	if limitBytes > 0 {
		limit = "its " + formatMemoryLimit(limitBytes) + " memory limit"
	}
	hint := "Resize the machine."
	if taskMachine {
		hint = "Resize the machine or raise the task machine memory."
	}
	return memoryLimitReasonPrefix + "the " + resourceLimitContainerName(service) + " reached " + limit + ". " + hint
}

// DiskLimitMessage explains why a machine stopped at its workspace disk limit.
func DiskLimitMessage(diskGB int) string {
	return fmt.Sprintf("%sthe workspace reached its %d GB disk limit, so Kuayle stopped the machine. Resize the disk to continue.", diskLimitReasonPrefix, diskGB)
}

// IsResourceLimitReason reports whether a run error or task reason is one of
// the resource limit explanations above, which are kept verbatim for users.
func IsResourceLimitReason(reason string) bool {
	return strings.HasPrefix(reason, memoryLimitReasonPrefix) || strings.HasPrefix(reason, diskLimitReasonPrefix)
}

func resourceLimitContainerName(service string) string {
	switch service {
	case "agent":
		return "agent container"
	case "validation":
		return "check container"
	case "compose":
		return "check service container"
	case "ide":
		return "editor container"
	case "docker":
		return "Docker daemon container"
	case "terminal", "browser", "collector", "egress":
		return service + " container"
	default:
		return "machine container"
	}
}

// formatMemoryLimit renders bytes as GiB with one decimal, or MiB below 1 GiB.
func formatMemoryLimit(bytes int64) string {
	const mib = 1024 * 1024
	if bytes < 1024*mib {
		return strconv.FormatInt(bytes/mib, 10) + " MiB"
	}
	tenths := (bytes*10 + 512*mib) / (1024 * mib)
	if tenths%10 == 0 {
		return strconv.FormatInt(tenths/10, 10) + " GiB"
	}
	return strconv.FormatInt(tenths/10, 10) + "." + strconv.FormatInt(tenths%10, 10) + " GiB"
}

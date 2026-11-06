// Package systemdslice sets the memory and CPU limits of a machine's systemd slice.
// The machine manager runs it in a short-lived helper container that only
// receives systemd's private socket, because the manager itself runs
// unprivileged: see machine.dockerMachineSlices.
package systemdslice

import (
	"context"
	"errors"
	"fmt"
	"os"
	"regexp"
	"strconv"
	"time"

	"github.com/godbus/dbus/v5"
)

// PrivateSocket is systemd's private D-Bus socket; it accepts root only.
const PrivateSocket = "/run/systemd/private"

// Command is the machine-manager argument that runs a slice operation.
const Command = "machine-slice"

var sliceName = regexp.MustCompile(`^kuayle-[a-z0-9]{1,64}\.slice$`)

// ValidName reports whether name is a Kuayle machine slice: one child of
// kuayle.slice per machine. Nothing else may be changed through the helper.
func ValidName(name string) bool {
	return sliceName.MatchString(name)
}

// Run executes one operation from the helper's arguments:
//
//	probe
//	apply <slice> <memory-bytes> <swap-bytes> <cpu-millis>
//	remove <slice>
func Run(ctx context.Context, args []string) error {
	if len(args) == 0 {
		return errors.New("missing operation")
	}
	switch operation := args[0]; {
	case operation == "probe" && len(args) == 1:
		return withManager(ctx, func(manager dbus.BusObject) error {
			_, err := manager.GetProperty("org.freedesktop.systemd1.Manager.Version")
			return err
		})
	case operation == "apply" && len(args) == 5:
		limits, err := parseLimits(args[2], args[3], args[4])
		if err != nil {
			return err
		}
		return Apply(ctx, args[1], limits)
	case operation == "remove" && len(args) == 2:
		return Remove(ctx, args[1])
	default:
		return fmt.Errorf("unsupported operation %q", operation)
	}
}

// Limits are a machine slice's ceilings.
type Limits struct {
	MemoryBytes uint64
	SwapBytes   uint64
	// CPUMillis is the CPU time per second, in thousandths of a CPU.
	CPUMillis uint64
}

func parseLimits(memoryValue, swapValue, cpuValue string) (Limits, error) {
	memory, err := strconv.ParseUint(memoryValue, 10, 64)
	if err != nil || memory < 128<<20 {
		return Limits{}, fmt.Errorf("invalid memory limit %q", memoryValue)
	}
	swap, err := strconv.ParseUint(swapValue, 10, 64)
	if err != nil || swap > memory {
		return Limits{}, fmt.Errorf("invalid swap limit %q", swapValue)
	}
	cpu, err := strconv.ParseUint(cpuValue, 10, 64)
	if err != nil || cpu < 10 || cpu > 1_000_000 {
		return Limits{}, fmt.Errorf("invalid CPU limit %q", cpuValue)
	}
	return Limits{MemoryBytes: memory, SwapBytes: swap, CPUMillis: cpu}, nil
}

type property struct {
	Name  string
	Value dbus.Variant
}

// Apply sets the slice's memory, swap and CPU ceilings (CPUQuota, as CPU
// time per second). The properties are stored persistently (as `systemctl
// set-property` does), so they also apply to a slice that does not exist yet
// and survive a host reboot.
func Apply(ctx context.Context, name string, limits Limits) error {
	if !ValidName(name) {
		return fmt.Errorf("invalid slice name %q", name)
	}
	properties := []property{
		{Name: "MemoryMax", Value: dbus.MakeVariant(limits.MemoryBytes)},
		{Name: "MemorySwapMax", Value: dbus.MakeVariant(limits.SwapBytes)},
		{Name: "CPUQuotaPerSecUSec", Value: dbus.MakeVariant(limits.CPUMillis * 1000)},
	}
	return withManager(ctx, func(manager dbus.BusObject) error {
		return manager.CallWithContext(ctx, "org.freedesktop.systemd1.Manager.SetUnitProperties", 0, name, false, properties).Err
	})
}

// Remove stops the slice (its containers are already gone) and deletes the
// stored properties.
func Remove(ctx context.Context, name string) error {
	if !ValidName(name) {
		return fmt.Errorf("invalid slice name %q", name)
	}
	return withManager(ctx, func(manager dbus.BusObject) error {
		var job dbus.ObjectPath
		if err := manager.CallWithContext(ctx, "org.freedesktop.systemd1.Manager.StopUnit", 0, name, "replace").Store(&job); err != nil && !noSuchUnit(err) {
			return err
		}
		var changes [][]any
		return manager.CallWithContext(ctx, "org.freedesktop.systemd1.Manager.RevertUnitFiles", 0, []string{name}).Store(&changes)
	})
}

func noSuchUnit(err error) bool {
	var dbusErr dbus.Error
	return errors.As(err, &dbusErr) && dbusErr.Name == "org.freedesktop.systemd1.NoSuchUnit"
}

func withManager(ctx context.Context, use func(dbus.BusObject) error) error {
	if _, ok := ctx.Deadline(); !ok {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, 30*time.Second)
		defer cancel()
	}
	connection, err := dbus.Dial("unix:path="+PrivateSocket, dbus.WithContext(ctx))
	if err != nil {
		return fmt.Errorf("connect to systemd: %w", err)
	}
	defer connection.Close()
	if err := connection.Auth([]dbus.Auth{dbus.AuthExternal(strconv.Itoa(os.Getuid()))}); err != nil {
		return fmt.Errorf("authenticate to systemd: %w", err)
	}
	return use(connection.Object("org.freedesktop.systemd1", "/org/freedesktop/systemd1"))
}

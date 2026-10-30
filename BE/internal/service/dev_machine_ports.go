package service

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"

	"github.com/google/uuid"
	"github.com/kuayle/kuayle-backend/internal/domain"
	"github.com/kuayle/kuayle-backend/internal/dto"
	"github.com/kuayle/kuayle-backend/internal/repository"
)

var (
	ErrPortNotFound = errors.New("app port not found")
	ErrPortExists   = errors.New("the machine already exposes this port")
	ErrPortReserved = errors.New("the port is used by one of the machine's own services")
)

// portOwner returns the machine when the caller may manage its app ports: the
// gateway only admits the machine's creator, so only they may manage them.
func (s *DevMachineService) portOwner(ctx context.Context, workspaceID, machineID, userID uuid.UUID) (*domain.DevMachine, error) {
	if _, err := s.enabledPolicy(ctx, workspaceID); err != nil {
		return nil, err
	}
	return s.GetForUser(ctx, workspaceID, machineID, userID)
}

// AppPortHost is the gateway host of a machine's app port.
func (s *DevMachineService) AppPortHost(machine *domain.DevMachine, port int) string {
	return machine.RoutingKey + "-" + strconv.Itoa(port) + "." + s.domain
}

func (s *DevMachineService) appPort(machine *domain.DevMachine, port domain.DevMachinePort) dto.AppPort {
	return dto.AppPort{
		ID: port.ID, Port: port.Port, Label: port.Label, Source: port.Source, AgentTaskID: port.AgentTaskID,
		TaskIdentifier: port.TaskIdentifier, Host: s.AppPortHost(machine, port.Port),
		Listening: slices.Contains(machine.ListeningPorts, int64(port.Port)), Target: port.Target, CreatedAt: port.CreatedAt,
	}
}

// ListPorts returns a machine's app ports and its detected listening ports, to
// the machine's creator or a workspace administrator.
func (s *DevMachineService) ListPorts(ctx context.Context, workspaceID, machineID, userID uuid.UUID, admin bool) (*dto.AppPortList, error) {
	if _, err := s.enabledPolicy(ctx, workspaceID); err != nil {
		return nil, err
	}
	var machine *domain.DevMachine
	var err error
	if admin {
		machine, err = s.store.GetMachine(ctx, workspaceID, machineID)
		if err == nil && machine == nil {
			err = ErrMachineNotFound
		}
	} else {
		machine, err = s.GetForUser(ctx, workspaceID, machineID, userID)
	}
	if err != nil {
		return nil, err
	}
	ports, err := s.store.ListPorts(ctx, workspaceID, machine.ID)
	if err != nil {
		return nil, err
	}
	list := &dto.AppPortList{Ports: make([]dto.AppPort, 0, len(ports)), Listening: make([]int, 0, len(machine.ListeningPorts)), ScannedAt: machine.ListeningPortsAt}
	for _, port := range ports {
		list.Ports = append(list.Ports, s.appPort(machine, port))
	}
	for _, port := range machine.ListeningPorts {
		list.Listening = append(list.Listening, int(port))
	}
	return list, nil
}

// normalizePortLabel trims a label; empty means none.
func normalizePortLabel(label *string) (*string, error) {
	if label == nil {
		return nil, nil
	}
	value := strings.TrimSpace(*label)
	if value == "" {
		return nil, nil
	}
	if !domain.ValidPortLabel(value) {
		return nil, fmt.Errorf("%w: a label has at most 40 letters, digits, spaces, dots, dashes or underscores", ErrInvalidMachineInput)
	}
	return &value, nil
}

// CreatePort exposes one port of the developer container. Ports of the
// machine's own services are refused, so an app port host can never reach
// code-server or the terminal without their dedicated protections.
func (s *DevMachineService) CreatePort(ctx context.Context, workspaceID, machineID, userID uuid.UUID, number int, label *string) (*dto.AppPort, error) {
	machine, err := s.portOwner(ctx, workspaceID, machineID, userID)
	if err != nil {
		return nil, err
	}
	if !domain.ValidAppPort(number) {
		return nil, fmt.Errorf("%w: the port must be between 1024 and 65535", ErrInvalidMachineInput)
	}
	if label, err = normalizePortLabel(label); err != nil {
		return nil, err
	}
	if err = s.ReservedPort(ctx, workspaceID, machine.ID, number); err != nil {
		return nil, err
	}
	port := &domain.DevMachinePort{WorkspaceID: workspaceID, MachineID: machine.ID, Port: number, Label: label, Source: domain.DevMachinePortSourceUser, CreatedByUserID: userID}
	if err = s.store.CreatePort(ctx, port); err != nil {
		return nil, portError(err)
	}
	s.emit(ctx, machine, nil, &userID, "gateway", "port.exposed", map[string]any{"port": number, "source": port.Source})
	view := s.appPort(machine, *port)
	return &view, nil
}

// ReservedPort refuses a port of one of the machine's own services.
func (s *DevMachineService) ReservedPort(ctx context.Context, workspaceID, machineID uuid.UUID, number int) error {
	services, err := s.store.ListServices(ctx, workspaceID, machineID)
	if err != nil {
		return err
	}
	for _, service := range services {
		if service.InternalPort == number {
			return fmt.Errorf("%w: port %d belongs to the machine's %s service", ErrPortReserved, number, service.ServiceType)
		}
	}
	return nil
}

func portError(err error) error {
	switch {
	case errors.Is(err, repository.ErrPortExists):
		return ErrPortExists
	}
	return err
}

// UpdatePort renames an app port.
func (s *DevMachineService) UpdatePort(ctx context.Context, workspaceID, machineID, portID, userID uuid.UUID, label *string) (*dto.AppPort, error) {
	machine, err := s.portOwner(ctx, workspaceID, machineID, userID)
	if err != nil {
		return nil, err
	}
	if label, err = normalizePortLabel(label); err != nil {
		return nil, err
	}
	port, err := s.store.UpdatePortLabel(ctx, workspaceID, machine.ID, portID, label)
	if err != nil {
		return nil, err
	}
	if port == nil {
		return nil, ErrPortNotFound
	}
	view := s.appPort(machine, *port)
	return &view, nil
}

// DeletePort removes an app port and revokes its browser sessions.
func (s *DevMachineService) DeletePort(ctx context.Context, workspaceID, machineID, portID, userID uuid.UUID) error {
	machine, err := s.portOwner(ctx, workspaceID, machineID, userID)
	if err != nil {
		return err
	}
	port, err := s.store.DeletePort(ctx, workspaceID, machine.ID, portID, userID)
	if err != nil {
		return err
	}
	if port == nil {
		return ErrPortNotFound
	}
	s.emit(ctx, machine, nil, &userID, "gateway", "port.removed", map[string]any{"port": port.Port})
	return nil
}

// LaunchPort issues a one-time ticket for an app port's host. The ticket names
// the running developer service and is bound to that host only.
func (s *DevMachineService) LaunchPort(ctx context.Context, workspaceID, machineID, portID, userID uuid.UUID) (*dto.LaunchServiceResponse, error) {
	machine, err := s.portOwner(ctx, workspaceID, machineID, userID)
	if err != nil {
		return nil, err
	}
	if s.domain == "" {
		return nil, fmt.Errorf("dev machine domain is not configured")
	}
	port, err := s.store.GetPort(ctx, workspaceID, machine.ID, portID)
	if err != nil {
		return nil, err
	}
	if port == nil {
		return nil, ErrPortNotFound
	}
	if pending, handled, err := s.pendingLaunch(ctx, workspaceID, machine, userID); handled || err != nil {
		return pending, err
	}
	services, err := s.store.ListServices(ctx, workspaceID, machine.ID)
	if err != nil {
		return nil, err
	}
	var developer *domain.DevMachineService
	for i := range services {
		service := &services[i]
		if service.InternalPort == port.Port {
			return nil, ErrPortReserved
		}
		if (service.ServiceType == "ide" || service.ServiceType == "terminal") && service.AgentRunID == nil && service.Status == "running" && service.ContainerID != nil {
			if developer == nil || service.ServiceType == "ide" {
				developer = service
			}
		}
	}
	if developer == nil {
		return nil, ErrServiceNotAvailable
	}
	launch := *developer
	launch.ServiceType = domain.AppPortService(port.Port)
	return s.issueLaunch(ctx, workspaceID, machine, &launch, userID, nil, "-"+strconv.Itoa(port.Port))
}

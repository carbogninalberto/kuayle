package machine

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sort"
	"time"

	"github.com/google/uuid"
	"github.com/kuayle/kuayle-backend/internal/domain"
	log "github.com/sirupsen/logrus"
)

// MachineImages are the installed release's images for a machine's own
// containers. An empty image leaves those containers on the image they have.
type MachineImages struct {
	IDE       string
	Headless  string
	Browser   string
	Collector string
	Egress    string
	Docker    string
}

// imageUpdateRetry is how long an update that found the machine busy, or
// failed, waits before reconciliation queues it again.
const imageUpdateRetry = time.Hour

// SetImages sets the installed release's machine images. New agent runs use
// the release's agent images at once; a running machine's own containers move
// to these images when it is idle, and a stopped machine's when it starts.
func (m *Manager) SetImages(images MachineImages) {
	m.images = images
}

type serviceImageChange struct {
	service domain.DevMachineService
	image   string
}

// releaseServiceImage is the installed release's image for one of a machine's
// own containers, or "" when the service keeps its image: an agent run's
// container, and the developer container of a machine built from a
// development environment, which is that environment's snapshot.
func (m *Manager) releaseServiceImage(machine *domain.DevMachine, service domain.DevMachineService) string {
	if isDeveloperService(service.ServiceType) && machine.EnvironmentID != nil {
		return ""
	}
	switch service.ServiceType {
	case "collector":
		return m.images.Collector
	case "egress":
		return m.images.Egress
	case "docker":
		return m.images.Docker
	case "browser":
		return m.images.Browser
	case "ide":
		return m.images.IDE
	case "terminal":
		// The IDE profile's terminal shares the IDE container.
		if machine.Profile == domain.DevMachineProfileHeadless {
			return m.images.Headless
		}
		return m.images.IDE
	}
	return ""
}

func (m *Manager) outdatedServices(machine *domain.DevMachine, services []domain.DevMachineService) []serviceImageChange {
	var changes []serviceImageChange
	for _, service := range services {
		if image := m.releaseServiceImage(machine, service); image != "" && image != service.ImageRef {
			changes = append(changes, serviceImageChange{service: service, image: image})
		}
	}
	return changes
}

// runImage is the image an agent run uses. A run that already has a container
// (resumed after a restart, or retried) keeps the image it started with; a new
// run of a built-in agent uses the installed release's image.
func (m *Manager) runImage(ctx context.Context, machine *domain.DevMachine, run *domain.DevMachineAgentRun, provider *domain.DevMachineAgentProvider, services []domain.DevMachineService) string {
	for _, service := range services {
		if service.AgentRunID != nil && *service.AgentRunID == run.ID && service.ServiceKey == "agent-"+run.ID.String() {
			return service.ImageRef
		}
	}
	if provider.IsCustom {
		return provider.ImageRef
	}
	registered, ok := m.agents.Get(provider.ProviderID)
	if !ok || registered.Metadata().DefaultImage == "" || registered.Metadata().DefaultImage == provider.ImageRef {
		return provider.ImageRef
	}
	image := registered.Metadata().DefaultImage
	if err := m.store.UpdateProviderImage(ctx, provider.ID, image); err != nil {
		log.WithFields(log.Fields{"machine_id": machine.ID, "provider_id": provider.ProviderID}).WithError(err).Warn("recording the agent's release image failed")
	}
	return image
}

// replaceOutdatedServices removes the containers of services whose image is
// not the installed release's and records the new image, so the next
// provisioning creates them from it. The workspace and Docker volumes stay;
// the containers keep nothing else (read-only root, in-memory /tmp and home).
func (m *Manager) replaceOutdatedServices(ctx context.Context, machine *domain.DevMachine, services []domain.DevMachineService) ([]serviceImageChange, error) {
	changes := m.outdatedServices(machine, services)
	if len(changes) == 0 {
		return nil, nil
	}
	outdated := make([]domain.DevMachineService, 0, len(changes))
	for _, change := range changes {
		outdated = append(outdated, change.service)
	}
	if err := m.runtime.RemoveContainers(ctx, machine, uniqueContainerIDs(outdated)); err != nil {
		return nil, fmt.Errorf("image_update_failed: %w", err)
	}
	for _, change := range changes {
		if err := m.store.ReplaceServiceImage(ctx, change.service.ID, change.image); err != nil {
			return nil, err
		}
	}
	return changes, nil
}

func (m *Manager) imagesUpdatedEvent(ctx context.Context, machine *domain.DevMachine, changes []serviceImageChange) error {
	updated := make([]map[string]string, 0, len(changes))
	for _, change := range changes {
		updated = append(updated, map[string]string{"service": change.service.ServiceKey, "from": change.service.ImageRef, "to": change.image})
	}
	return m.createEvent(ctx, machine, nil, "lifecycle", "machine.images_updated", map[string]any{"services": updated})
}

// updateImages moves an idle running machine to the installed release's
// images: it removes its finished runs' containers, then recreates its own
// containers that use another image. A machine that became busy since the
// update was queued is left for a later attempt.
func (m *Manager) updateImages(ctx context.Context, machine *domain.DevMachine, services []domain.DevMachineService, operation *domain.DevMachineOperation) error {
	if machine.Status != domain.DevMachineStatusRunning || machine.DesiredStatus != domain.DevMachineStatusRunning {
		return nil
	}
	busy, err := m.store.MachineBusyForImageUpdate(ctx, machine.ID, operation.ID)
	if err != nil || busy {
		return err
	}
	if err := m.runtime.RemoveFinishedRunContainers(ctx, machine); err != nil {
		log.WithField("machine_id", machine.ID).WithError(err).Warn("removing finished run containers failed")
	}
	changes, err := m.replaceOutdatedServices(ctx, machine, services)
	if err != nil || len(changes) == 0 {
		return err
	}
	if services, err = m.store.ListServices(ctx, machine.WorkspaceID, machine.ID); err != nil {
		return err
	}
	if err := m.spawn(ctx, machine, services, operation); err != nil {
		return err
	}
	return m.imagesUpdatedEvent(ctx, machine, changes)
}

// queueImageUpdate queues update_images for a running machine with outdated
// containers. The store queues it only while the machine is idle; the key
// changes with the target images and every imageUpdateRetry, so a busy
// machine is tried again later and a new release is applied at once.
func (m *Manager) queueImageUpdate(ctx context.Context, machine *domain.DevMachine, services []domain.DevMachineService, now time.Time) {
	changes := m.outdatedServices(machine, services)
	if len(changes) == 0 {
		return
	}
	targets := make([]string, 0, len(changes))
	for _, change := range changes {
		targets = append(targets, change.service.ServiceKey+"="+change.image)
	}
	sort.Strings(targets)
	digest := sha256.New()
	for _, target := range targets {
		digest.Write([]byte(target + "\n"))
	}
	operation := &domain.DevMachineOperation{
		ID: uuid.New(), MachineID: machine.ID, WorkspaceID: machine.WorkspaceID, MaxAttempts: 3,
		IdempotencyKey: fmt.Sprintf("update-images:%d:%s:%d", machine.Generation, hex.EncodeToString(digest.Sum(nil))[:16], now.Unix()/int64(imageUpdateRetry.Seconds())),
	}
	if _, err := m.store.EnqueueImageUpdate(ctx, operation); err != nil {
		log.WithField("machine_id", machine.ID).WithError(err).Warn("image update queue failed")
	}
}

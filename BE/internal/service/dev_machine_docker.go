package service

import (
	"fmt"

	"github.com/google/uuid"
	"github.com/kuayle/kuayle-backend/internal/domain"
	cryptoutil "github.com/kuayle/kuayle-backend/pkg/crypto"
	"github.com/kuayle/kuayle-backend/pkg/dockertls"
)

const (
	// dockerServiceType is the machine's Docker daemon service. Its server TLS
	// material is delivered to it as scoped secrets.
	dockerServiceType = "docker"
	// dockerClientTarget scopes the daemon's client TLS material, delivered
	// to developer and agent containers.
	dockerClientTarget = "docker-client"
	// dockerDaemonPort is the daemon's mutual-TLS API port.
	dockerDaemonPort = 2376
)

// dockerEnvVars encrypts a machine's daemon TLS material as scoped secrets
// that expire with the machine.
func (s *DevMachineService) dockerEnvVars(machine *domain.DevMachine) ([]domain.DevMachineEnvVar, error) {
	material, err := dockertls.New(machine.RoutingKey, machine.ExpiresAt)
	if err != nil {
		return nil, fmt.Errorf("issue docker TLS material: %w", err)
	}
	values := []struct{ target, name, value string }{
		{dockerServiceType, "KUAYLE_DOCKER_TLS_CA", material.CA},
		{dockerServiceType, "KUAYLE_DOCKER_TLS_CERT", material.ServerCert},
		{dockerServiceType, "KUAYLE_DOCKER_TLS_KEY", material.ServerKey},
		{dockerClientTarget, "KUAYLE_DOCKER_CA", material.CA},
		{dockerClientTarget, "KUAYLE_DOCKER_CERT", material.ClientCert},
		{dockerClientTarget, "KUAYLE_DOCKER_KEY", material.ClientKey},
	}
	envVars := make([]domain.DevMachineEnvVar, 0, len(values))
	for _, value := range values {
		encrypted, err := cryptoutil.Encrypt(value.value, s.encryptionKey)
		if err != nil {
			return nil, err
		}
		envVars = append(envVars, domain.DevMachineEnvVar{
			ID: uuid.New(), MachineID: machine.ID, TargetService: value.target, Name: value.name,
			EncryptedValue: encrypted, EncryptionKeyVersion: 1, IsSecret: true, ExpiresAt: &machine.ExpiresAt,
		})
	}
	return envVars, nil
}

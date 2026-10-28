package service

import (
	"context"
	"crypto/x509"
	"encoding/pem"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/kuayle/kuayle-backend/internal/agent"
	"github.com/kuayle/kuayle-backend/internal/dto"
	cryptoutil "github.com/kuayle/kuayle-backend/pkg/crypto"
	"github.com/stretchr/testify/require"
)

func parseCertificate(t *testing.T, value string) *x509.Certificate {
	t.Helper()
	block, _ := pem.Decode([]byte(value))
	require.NotNil(t, block)
	certificate, err := x509.ParseCertificate(block.Bytes)
	require.NoError(t, err)
	return certificate
}

// With the workspace switch on, a machine gets a docker service on the
// machine network and its TLS material as encrypted, machine-expiring
// secrets: the server half for the daemon, the client half for developer and
// agent containers. Without it there is no daemon.
func TestCreateAddsTheDockerDaemonOnlyWhenTheWorkspaceAllowsIt(t *testing.T) {
	for _, allowed := range []bool{false, true} {
		workspaceID, userID := uuid.New(), uuid.New()
		policy := testPolicy(workspaceID)
		policy.AllowDocker = allowed
		store := &devMachineStoreFake{policy: policy}
		svc := NewDevMachineService(store, agent.NewRegistry(), true, "machines.example.test", cryptoutil.DeriveKey("test"), time.Minute, DevMachineImages{Docker: "kuayle/dev-machine-docker:test"})
		machine, _, err := svc.Create(context.Background(), workspaceID, userID, dto.CreateDevMachineRequest{Name: "docker-machine", Size: "small", Services: dto.DevMachineServicesInput{IDE: true}})
		require.NoError(t, err)
		var docker []string
		for _, service := range store.createdServices {
			if service.ServiceType == "docker" {
				docker = append(docker, service.ServiceKey)
				require.Equal(t, "kuayle/dev-machine-docker:test", service.ImageRef)
				require.Equal(t, "kuayle-"+machine.RoutingKey+"-docker", service.ContainerName)
				require.Equal(t, machine.RoutingKey+"-docker", service.InternalHost)
				require.Equal(t, 2376, service.InternalPort)
			}
		}
		secrets := map[string]map[string]string{}
		for _, envVar := range store.createdEnvVars {
			if envVar.TargetService != dockerServiceType && envVar.TargetService != dockerClientTarget {
				continue
			}
			require.True(t, envVar.IsSecret)
			require.Equal(t, store.createdMachine.ExpiresAt, *envVar.ExpiresAt)
			value, err := cryptoutil.Decrypt(envVar.EncryptedValue, cryptoutil.DeriveKey("test"))
			require.NoError(t, err)
			if secrets[envVar.TargetService] == nil {
				secrets[envVar.TargetService] = map[string]string{}
			}
			secrets[envVar.TargetService][envVar.Name] = value
		}
		if !allowed {
			require.Empty(t, docker)
			require.Empty(t, secrets)
			continue
		}
		require.Equal(t, []string{"docker"}, docker)
		require.ElementsMatch(t, []string{"KUAYLE_DOCKER_TLS_CA", "KUAYLE_DOCKER_TLS_CERT", "KUAYLE_DOCKER_TLS_KEY"}, keys(secrets[dockerServiceType]))
		require.ElementsMatch(t, []string{"KUAYLE_DOCKER_CA", "KUAYLE_DOCKER_CERT", "KUAYLE_DOCKER_KEY"}, keys(secrets[dockerClientTarget]))
		require.Equal(t, secrets[dockerServiceType]["KUAYLE_DOCKER_TLS_CA"], secrets[dockerClientTarget]["KUAYLE_DOCKER_CA"])
		require.NotEqual(t, secrets[dockerServiceType]["KUAYLE_DOCKER_TLS_KEY"], secrets[dockerClientTarget]["KUAYLE_DOCKER_KEY"])
		server := parseCertificate(t, secrets[dockerServiceType]["KUAYLE_DOCKER_TLS_CERT"])
		require.Contains(t, server.DNSNames, machine.RoutingKey+"-docker")
	}
}

func keys(values map[string]string) []string {
	result := make([]string, 0, len(values))
	for key := range values {
		result = append(result, key)
	}
	return result
}

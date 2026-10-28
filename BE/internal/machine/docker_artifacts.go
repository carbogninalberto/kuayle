package machine

import (
	"archive/tar"
	"context"
	"errors"
	"io"
	"path"
	"strings"

	"github.com/containerd/errdefs"
	"github.com/kuayle/kuayle-backend/internal/domain"
	"github.com/moby/moby/client"
)

// Report directories conventionally written by Playwright and other runners.
var validationArtifactDirs = []string{"playwright-report", "test-results"}

var validationArtifactTypes = map[string]string{
	".png": "image/png", ".jpg": "image/jpeg", ".jpeg": "image/jpeg", ".zip": "application/zip",
	".html": "text/html", ".json": "application/json", ".xml": "application/xml",
	".txt": "text/plain", ".log": "text/plain", ".md": "text/plain", ".webm": "video/webm",
}

const (
	maxValidationArtifactSize  = 5 << 20
	maxValidationArtifactTotal = 20 << 20
	maxValidationArtifactCount = 100
)

// CollectValidationArtifacts reads report files from the exited independent
// check container. Only regular files of allowed types under the report
// directories are kept, within per-file, total and count limits; links,
// devices and anything outside those directories are ignored.
func (r *DockerRuntime) CollectValidationArtifacts(ctx context.Context, machine *domain.DevMachine, run *domain.DevMachineAgentRun, checkout *domain.DevMachineCheckout) ([]domain.ValidationArtifact, error) {
	if checkout == nil {
		return nil, nil
	}
	name := "kuayle-" + machine.RoutingKey + "-validation-" + run.ID.String()
	artifacts := []domain.ValidationArtifact{}
	total := 0
	for _, dir := range validationArtifactDirs {
		result, err := r.client.CopyFromContainer(ctx, name, client.CopyFromContainerOptions{SourcePath: checkout.WorkspacePath + "/" + dir})
		if errdefs.IsNotFound(err) {
			continue
		}
		if err != nil {
			return artifacts, err
		}
		archive := tar.NewReader(result.Content)
		for len(artifacts) < maxValidationArtifactCount {
			header, err := archive.Next()
			if errors.Is(err, io.EOF) {
				break
			}
			if err != nil {
				result.Content.Close()
				return artifacts, err
			}
			clean := path.Clean(header.Name)
			if header.Typeflag != tar.TypeReg || header.Size > maxValidationArtifactSize || total+int(header.Size) > maxValidationArtifactTotal ||
				!strings.HasPrefix(clean, dir+"/") || strings.Contains(clean, "..") || len(clean) > 512 {
				continue
			}
			contentType, allowed := validationArtifactTypes[strings.ToLower(path.Ext(clean))]
			if !allowed {
				continue
			}
			data, err := io.ReadAll(io.LimitReader(archive, maxValidationArtifactSize+1))
			if err != nil || len(data) > maxValidationArtifactSize {
				continue
			}
			total += len(data)
			artifacts = append(artifacts, domain.ValidationArtifact{Path: clean, ContentType: contentType, Size: len(data), Data: data})
		}
		result.Content.Close()
	}
	return artifacts, nil
}

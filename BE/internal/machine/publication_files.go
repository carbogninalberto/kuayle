package machine

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"net/url"
	"strconv"
	"strings"

	"github.com/google/uuid"
	"github.com/kuayle/kuayle-backend/internal/domain"
	githubclient "github.com/kuayle/kuayle-backend/pkg/github"
	log "github.com/sirupsen/logrus"
)

// agentFileReadScript prints "F <base64>" with at most $3 bytes of
// <worktree>/.kuayle/$2, "F invalid" when it is not a regular file, and
// nothing when it is missing. The worktree, .kuayle and the file's directory
// are never followed through symlinks, and the file stays in place: a retried
// publication reads it again, and the next attempt's start removes it
// (prepareOutboxScript). The container's sh is dash.
const agentFileReadScript = `set -u
root=$1 name=$2 limit=$3
[ -d "$root" ] && [ ! -L "$root" ] || exit 0
[ -d "$root/.kuayle" ] && [ ! -L "$root/.kuayle" ] || exit 0
case "$name" in */*) dir="$root/.kuayle/${name%/*}"; [ -d "$dir" ] && [ ! -L "$dir" ] || exit 0 ;; esac
f="$root/.kuayle/$name"
[ -e "$f" ] || [ -L "$f" ] || exit 0
if [ -L "$f" ] || [ ! -f "$f" ]; then printf 'F invalid\n'; exit 0; fi
printf 'F %s\n' "$(head -c "$limit" "$f" | base64 | tr -d '\n')"
exit 0`

var errAgentFileInvalid = errors.New("the file is not a regular file")

// agentFileRuntime is implemented by runtimes that can read Kuayle's own
// files in a task worktree. ReadAgentFile returns nil without the file.
type agentFileRuntime interface {
	ReadAgentFile(ctx context.Context, services []domain.DevMachineService, checkout *domain.DevMachineCheckout, name string, limit int) ([]byte, error)
}

// screenshotPublisher is implemented by GitHub clients that can commit files
// to a branch of their own.
type screenshotPublisher interface {
	PublishFilesToBranch(token, owner, repo, branch, message string, files []githubclient.RepositoryFile) (string, error)
}

func parseAgentFileRead(output string) ([]byte, error) {
	line := strings.TrimSpace(output)
	if !strings.HasPrefix(line, "F") {
		return nil, nil
	}
	value := strings.TrimSpace(strings.TrimPrefix(line, "F"))
	if value == "invalid" {
		return nil, errAgentFileInvalid
	}
	data, err := base64.StdEncoding.DecodeString(value)
	if err != nil {
		return nil, errors.New("the file could not be read")
	}
	if data == nil {
		data = []byte{}
	}
	return data, nil
}

// validAgentFileName is the pull request file or one screenshot.
func validAgentFileName(name string) bool {
	shot, isShot := strings.CutPrefix(name, "screenshots/")
	return name == "pull_request.json" || isShot && domain.ValidAgentScreenshotName(shot)
}

// ReadAgentFile reads one of Kuayle's files in a task worktree as the
// developer container's user, so it reads only what the agent itself could.
func (r *DockerRuntime) ReadAgentFile(ctx context.Context, services []domain.DevMachineService, checkout *domain.DevMachineCheckout, name string, limit int) ([]byte, error) {
	developer := developerContainerID(services)
	if developer == "" || checkout == nil || !strings.HasPrefix(checkout.WorkspacePath, taskWorktreeRoot+"/") || !validAgentFileName(name) || limit <= 0 {
		return nil, nil
	}
	output, err := r.execOutput(ctx, developer, []string{"/bin/sh", "-c", agentFileReadScript, "kuayle-file", checkout.WorkspacePath, name, strconv.Itoa(limit)})
	if err != nil {
		return nil, err
	}
	return parseAgentFileRead(output)
}

// screenshotBranch holds a task's screenshots, apart from its code.
func screenshotBranch(taskID uuid.UUID) string {
	return "kuayle/screenshots/" + taskID.String()
}

// screenshotURL shows a file of the screenshots commit to whoever can read
// the repository: GitHub serves ?raw=true with the viewer's own access.
func screenshotURL(owner, repository, commit, name string) string {
	return fmt.Sprintf("https://github.com/%s/%s/blob/%s/%s?raw=true", url.PathEscape(owner), url.PathEscape(repository), commit, url.PathEscape(name))
}

// pullRequestFiles reads what the attempt left for its pull request and
// publishes its screenshots. Everything here is best-effort: a missing,
// rejected or unreadable file, or screenshots GitHub refuses, leave the
// description to Kuayle's facts and never fail the publication.
func (m *Manager) pullRequestFiles(ctx context.Context, github githubAPI, token string, run *domain.DevMachineAgentRun, services []domain.DevMachineService, checkout *domain.DevMachineCheckout, publication *domain.AgentPublication) pullRequestContent {
	runtime, ok := m.runtime.(agentFileRuntime)
	if !ok || run.TaskID == nil {
		return pullRequestContent{}
	}
	logger := log.WithFields(log.Fields{"agent_run_id": run.ID, "task_id": *run.TaskID})
	data, err := runtime.ReadAgentFile(ctx, services, checkout, "pull_request.json", domain.AgentPullRequestFileMaxBytes+1)
	if err != nil {
		logger.WithError(err).Warn("agent pull request file could not be read")
		return pullRequestContent{}
	}
	if data == nil {
		return pullRequestContent{}
	}
	parsed := domain.ParseAgentPullRequestFile(data, len(data) > domain.AgentPullRequestFileMaxBytes)
	if parsed.Error != "" {
		logger.WithField("reason", parsed.Error).Warn("agent pull request file was rejected")
		return pullRequestContent{}
	}
	content := pullRequestContent{file: &parsed}
	publisher, publishes := github.(screenshotPublisher)
	if len(parsed.Screenshots) == 0 || !publishes {
		return content
	}
	owner, repository, _ := splitRepositoryFullName(checkout.RepositoryFullName)
	files := make([]githubclient.RepositoryFile, 0, len(parsed.Screenshots))
	captions := make([]string, 0, len(parsed.Screenshots))
	for _, shot := range parsed.Screenshots {
		image, err := runtime.ReadAgentFile(ctx, services, checkout, "screenshots/"+shot.File, domain.AgentScreenshotMaxBytes+1)
		if err == nil && image == nil {
			err = errors.New("the file does not exist")
		}
		if err == nil {
			_, err = domain.ScreenshotContentType(shot.File, image)
		}
		if err != nil {
			logger.WithField("screenshot", shot.File).WithError(err).Warn("agent screenshot was left out")
			continue
		}
		files = append(files, githubclient.RepositoryFile{Path: shot.File, Content: image})
		captions = append(captions, shot.Caption)
	}
	if len(files) == 0 {
		return content
	}
	commit, err := publisher.PublishFilesToBranch(token, owner, repository, screenshotBranch(*run.TaskID), publication.IssueIdentifier+": screenshots for the pull request description", files)
	if err != nil {
		logger.WithField("error", strings.ReplaceAll(safeError(err), token, "[redacted]")).Warn("agent screenshots could not be published")
		return content
	}
	for i, file := range files {
		content.screenshots = append(content.screenshots, publishedScreenshot{URL: screenshotURL(owner, repository, commit, file.Path), Caption: captions[i]})
	}
	return content
}

var (
	_ agentFileRuntime    = (*DockerRuntime)(nil)
	_ screenshotPublisher = (*githubclient.Client)(nil)
)

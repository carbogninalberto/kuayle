package service

import (
	"context"
	"errors"
	"fmt"
	"html"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/kuayle/kuayle-backend/internal/domain"
	"github.com/kuayle/kuayle-backend/internal/importer"
	"github.com/kuayle/kuayle-backend/internal/repository"
	"github.com/kuayle/kuayle-backend/pkg/sanitize"
)

// importRun holds the mutable state of one job execution. Keeping it off the
// service makes concurrent jobs independent.
type importRun struct {
	svc           *ImportService
	ctx           context.Context
	job           *domain.ImportJob
	source        importer.Source
	mapping       importer.Mapping
	members       []importer.Member
	teams         map[uuid.UUID]domain.Team
	importerID    uuid.UUID
	slug          string
	statuses      map[uuid.UUID][]domain.TeamStatus
	refs          map[string]*domain.ImportExternalRef
	delta         repository.ImportCounterDelta
	sinceFlush    int
	lastHeartbeat time.Time
}

type preparedIssue struct {
	source           importer.SourceIssue
	teamID           uuid.UUID
	projectID        *uuid.UUID
	projectOverride  *uuid.UUID
	cycleID          *uuid.UUID
	labelIDs         []uuid.UUID
	parentExternalID string
	hash             string
	prepErr          error
}

// relationWork and parentWork are written after every issue exists, so both
// ends can be resolved. They carry the child's report key so a failure is
// reported against the issue that owns the link.
type relationWork struct {
	issueKey string
	rel      importer.SourceRelation
}

type parentWork struct {
	childID          uuid.UUID
	childExternalID  string
	childKey         string
	parentExternalID string
}

// importedAttachment is a stored asset linked from an imported description.
type importedAttachment struct {
	assetID  uuid.UUID
	filename string
	size     int64
}

func refKey(extType, extID string) string { return extType + "\x00" + extID }

// reportKey is what the admin sees in the item report: the source's own key
// (PROJ-123), so a failing item can be found in Jira.
func reportKey(issue importer.SourceIssue) string {
	if issue.ExternalKey != "" {
		return issue.ExternalKey
	}
	return issue.ExternalID
}

// heartbeat keeps the job's updated_at fresh so stale recovery never fails a
// live job. It is throttled to at most once every 30 seconds; force sends one
// immediately (used before a potentially long attachment download).
func (r *importRun) heartbeat(force bool) {
	now := r.svc.now()
	if !force && !r.lastHeartbeat.IsZero() && now.Sub(r.lastHeartbeat) < 30*time.Second {
		return
	}
	r.lastHeartbeat = now
	if err := r.svc.repo.Heartbeat(r.ctx, r.job.ID); err != nil {
		r.svc.logger.WithError(err).Warn("could not heartbeat import job")
	}
}

func (s *ImportService) process(ctx context.Context, job *domain.ImportJob) {
	finish := func(status, message string) {
		var jobError *string
		if message != "" {
			jobError = &message
		}
		if err := s.repo.Finish(context.WithoutCancel(ctx), job.ID, status, jobError); err != nil {
			s.logger.WithError(err).Error("could not finish import job")
		}
	}
	defer func() {
		// Credentials are wiped on every exit path, including a panic.
		if err := s.repo.WipeCredentials(context.WithoutCancel(ctx), job.ID); err != nil {
			s.logger.WithError(err).Warn("could not wipe import credentials")
		}
		if recovered := recover(); recovered != nil {
			finish(domain.ImportStatusFailed, fmt.Sprintf("import panicked: %v", recovered))
		}
	}()

	if job.CreatedBy == nil {
		finish(domain.ImportStatusFailed, errImporterGone.Error())
		return
	}
	source, err := s.sourceFor(ctx, job)
	if err != nil {
		finish(domain.ImportStatusFailed, err.Error())
		return
	}
	mapping, err := decodeMapping(job.Mapping)
	if err != nil {
		finish(domain.ImportStatusFailed, err.Error())
		return
	}
	// Repositories apply team/issue/asset visibility from the access context;
	// stamping the admin's identity keeps private-team writes authorised.
	workCtx := domain.WithAccess(ctx, domain.Access{UserID: *job.CreatedBy, WorkspaceID: job.WorkspaceID})
	// Only admins import. The role is read again when the job runs, so an
	// admin demoted after queueing the job cannot write through it.
	role, err := s.store.MemberRole(workCtx, job.WorkspaceID, *job.CreatedBy)
	if err != nil {
		finish(domain.ImportStatusFailed, err.Error())
		return
	}
	if role != domain.RoleOwner && role != domain.RoleAdmin {
		finish(domain.ImportStatusFailed, errImporterNotAdmin.Error())
		return
	}
	members, err := s.store.Members(workCtx, job.WorkspaceID)
	if err != nil {
		finish(domain.ImportStatusFailed, err.Error())
		return
	}
	teams, err := s.store.Teams(workCtx, job.WorkspaceID)
	if err != nil {
		finish(domain.ImportStatusFailed, err.Error())
		return
	}
	slug, err := s.store.WorkspaceSlug(workCtx, job.WorkspaceID)
	if err != nil {
		finish(domain.ImportStatusFailed, err.Error())
		return
	}
	teamMap := make(map[uuid.UUID]domain.Team, len(teams))
	for _, team := range teams {
		teamMap[team.ID] = team
	}
	run := &importRun{
		svc: s, ctx: workCtx, job: job, source: source, mapping: mapping, members: members, teams: teamMap,
		importerID: *job.CreatedBy, slug: slug,
		statuses: map[uuid.UUID][]domain.TeamStatus{}, refs: map[string]*domain.ImportExternalRef{},
	}
	if err := run.execute(); err != nil {
		_ = run.flush()
		if errors.Is(err, ErrImportCancelled) {
			finish(domain.ImportStatusCancelled, "")
		} else {
			finish(domain.ImportStatusFailed, sourceErrorMessage(err))
		}
		return
	}
	if err := run.flush(); err != nil {
		finish(domain.ImportStatusFailed, err.Error())
		return
	}
	finish(domain.ImportStatusCompleted, "")
}

func (r *importRun) execute() error {
	var issues []importer.SourceIssue
	emitted := 0
	if err := r.source.Fetch(r.ctx, func(issue importer.SourceIssue) error {
		issues = append(issues, issue)
		emitted++
		// A cancel during a long fetch must stop before the write phase.
		// %w keeps ErrImportCancelled detectable through Fetch's return.
		if emitted%25 == 0 {
			cancelled, err := r.svc.repo.IsCancelRequested(r.ctx, r.job.ID)
			if err != nil {
				return err
			}
			if cancelled {
				return ErrImportCancelled
			}
		}
		r.heartbeat(false)
		return nil
	}); err != nil {
		if errors.Is(err, ErrImportCancelled) {
			return err
		}
		return &importFetchError{err: err}
	}
	prepared := r.prepare(issues)

	parents := []parentWork{}
	relations := []relationWork{}
	for _, item := range prepared {
		if err := r.tick(); err != nil {
			return err
		}
		key := reportKey(item.source)
		if item.prepErr != nil {
			r.item(domain.ImportExternalIssue, item.source.ExternalID, key, domain.ImportOutcomeFailed, item.prepErr.Error())
			continue
		}
		issueID, err := r.writeIssue(item)
		if err != nil {
			r.item(domain.ImportExternalIssue, item.source.ExternalID, key, domain.ImportOutcomeFailed, err.Error())
			continue
		}
		if item.parentExternalID != "" {
			parents = append(parents, parentWork{
				childID: issueID, childExternalID: item.source.ExternalID, childKey: key,
				parentExternalID: item.parentExternalID,
			})
		}
		for _, link := range item.source.Relations {
			relations = append(relations, relationWork{issueKey: key, rel: link})
		}
	}

	// Parent links are reported against the child under their own type: the
	// child was already counted as created/updated/skipped, so a link problem
	// must not move the issue counters or blame the parent.
	for _, work := range parents {
		target, err := r.ref(domain.ImportExternalIssue, work.parentExternalID)
		if err != nil {
			r.item(domain.ImportReportParent, work.childExternalID, work.childKey, domain.ImportOutcomeFailed, err.Error())
			continue
		}
		if target == nil {
			r.item(domain.ImportReportParent, work.childExternalID, work.childKey, domain.ImportOutcomeSkipped,
				"parent issue "+work.parentExternalID+" was not imported")
			continue
		}
		if err := r.svc.store.SetParent(r.ctx, r.job.WorkspaceID, work.childID, target.KuayleID, r.importerID); err != nil {
			r.item(domain.ImportReportParent, work.childExternalID, work.childKey, domain.ImportOutcomeFailed, err.Error())
		}
	}

	for _, work := range relations {
		if err := r.writeRelation(work); err != nil {
			r.item(domain.ImportExternalRelation, work.rel.ExternalID, work.issueKey, domain.ImportOutcomeFailed, err.Error())
		}
	}
	return nil
}

// currentSprint picks the sprint an issue is in now: the active one, else a
// planned one, else the latest it was in. Kuayle has one cycle per issue.
func currentSprint(sprints []importer.SourceSprint) (importer.SourceSprint, bool) {
	if len(sprints) == 0 {
		return importer.SourceSprint{}, false
	}
	for _, state := range []string{importer.SprintStateActive, importer.SprintStateFuture} {
		for _, sprint := range sprints {
			if sprint.State == state {
				return sprint, true
			}
		}
	}
	return sprints[len(sprints)-1], true
}

// prepare resolves containers (labels, sprints, epics) before issues are
// written, so each issue can be created with its final references in one
// short transaction. Container errors are stored on the prepared issue and
// reported as a per-item failure by execute, never aborting the whole job.
func (r *importRun) prepare(issues []importer.SourceIssue) []preparedIssue {
	out := make([]preparedIssue, 0, len(issues))
	epicProjects := map[string]*uuid.UUID{}
	type cycleKey struct {
		teamID uuid.UUID
		name   string
	}
	type cacheEntry[T any] struct {
		value T
		err   error
	}
	cycleCache := map[cycleKey]cacheEntry[uuid.UUID]{}
	labelCache := map[string]cacheEntry[uuid.UUID]{}
	for _, issue := range issues {
		prepared := preparedIssue{source: issue, hash: importer.ContentHash(issue)}
		target, ok := r.mapping.Projects[issue.ProjectExternalID]
		if !ok || target.TeamID == uuid.Nil {
			// writeIssue reports ErrNoProjectMapping for this issue. Skip
			// container resolution entirely: a nil team would make a real
			// EnsureCycle/EnsureProject fail and abort the job.
			out = append(out, prepared)
			continue
		}
		prepared.teamID = target.TeamID
		prepared.projectID = target.ProjectID

		for _, name := range issue.Labels {
			result, found := labelCache[strings.ToLower(name)]
			if !found {
				result.value, result.err = r.svc.store.EnsureLabel(r.ctx, r.job.WorkspaceID, nil, name)
				labelCache[strings.ToLower(name)] = result
			}
			if result.err != nil {
				prepared.prepErr = result.err
				break
			}
			prepared.labelIDs = append(prepared.labelIDs, result.value)
		}
		if prepared.prepErr == nil && r.mapping.SprintMode != importer.SprintModeIgnore {
			if sprint, ok := currentSprint(issue.Sprints); ok {
				key := cycleKey{teamID: prepared.teamID, name: strings.ToLower(sprint.Name)}
				result, found := cycleCache[key]
				if !found {
					result.value, result.err = r.svc.store.EnsureCycle(r.ctx, r.job.WorkspaceID, prepared.teamID, sprint)
					cycleCache[key] = result
				}
				if result.err != nil {
					prepared.prepErr = result.err
				} else {
					cycleID := result.value
					prepared.cycleID = &cycleID
				}
			}
		}
		if prepared.prepErr == nil && issue.EpicExternalID != "" {
			switch r.mapping.EpicMode {
			case importer.EpicModeParent:
				prepared.parentExternalID = issue.EpicExternalID
			case importer.EpicModeProject:
				if id, ok := epicProjects[issue.EpicExternalID]; ok {
					prepared.projectOverride = id
				} else {
					projectID, err := r.svc.store.EnsureProject(r.ctx, r.job.WorkspaceID, prepared.teamID, "Epic "+issue.EpicExternalID)
					if err != nil {
						prepared.prepErr = err
					} else {
						epicProjects[issue.EpicExternalID] = &projectID
						prepared.projectOverride = &projectID
					}
				}
			}
		}
		if prepared.parentExternalID == "" {
			prepared.parentExternalID = issue.ParentExternalID
		}
		out = append(out, prepared)
	}
	return out
}

func (r *importRun) teamStatuses(teamID uuid.UUID) []domain.TeamStatus {
	statuses, ok := r.statuses[teamID]
	if !ok {
		list, err := r.svc.store.Statuses(r.ctx, r.job.WorkspaceID, teamID)
		if err != nil {
			return nil
		}
		statuses = list
		r.statuses[teamID] = list
	}
	return statuses
}

func (r *importRun) statusForTeam(teamID uuid.UUID) (domain.TeamStatus, bool) {
	statuses := r.teamStatuses(teamID)
	for _, status := range statuses {
		if status.IsDefault {
			return status, true
		}
	}
	if len(statuses) > 0 {
		return statuses[0], true
	}
	return domain.TeamStatus{}, false
}

// resolveStatus returns the team status for a source status: the mapped one
// when it belongs to this team, otherwise the team's default.
func (r *importRun) resolveStatus(teamID uuid.UUID, source string) (uuid.UUID, error) {
	if status, mapped := r.mapping.ResolveStatus(teamID, source); mapped && status.TeamID == teamID {
		return status.StatusID, nil
	}
	if fallback, found := r.statusForTeam(teamID); found {
		return fallback.ID, nil
	}
	return uuid.Nil, fmt.Errorf("no status available for team %s", teamID)
}

func issueStatus(category domain.StatusCategory) domain.IssueStatus {
	switch category {
	case domain.StatusCategoryBacklog:
		return domain.IssueStatusBacklog
	case domain.StatusCategoryUnstarted:
		return domain.IssueStatusTodo
	case domain.StatusCategoryStarted:
		return domain.IssueStatusInProgress
	case domain.StatusCategoryCompleted:
		return domain.IssueStatusDone
	case domain.StatusCategoryCancelled:
		return domain.IssueStatusCancelled
	default:
		return domain.IssueStatusTodo
	}
}

func optionalID(id *uuid.UUID) string {
	if id == nil {
		return ""
	}
	return id.String()
}

func (r *importRun) writeIssue(item preparedIssue) (uuid.UUID, error) {
	issue := item.source
	target, ok := r.mapping.Projects[issue.ProjectExternalID]
	if !ok || target.TeamID == uuid.Nil {
		return uuid.Nil, importer.ErrNoProjectMapping
	}
	statusID, err := r.resolveStatus(target.TeamID, issue.Status)
	if err != nil {
		return uuid.Nil, err
	}
	priority, _ := r.mapping.ResolvePriority(issue.Priority)
	creatorID, unmatchedAuthor := r.mapping.ResolveUser(issue.Reporter, r.members, r.importerID)
	// Assignees resolve differently from authors: a missing, unmatched or
	// explicitly unassigned source user leaves the issue with no assignee.
	assigneeID := r.mapping.ResolveAssignee(issue.Assignee, r.members)
	projectID := target.ProjectID
	if item.projectOverride != nil {
		projectID = item.projectOverride
	}
	labels := make([]string, 0, len(item.labelIDs))
	for _, id := range item.labelIDs {
		labels = append(labels, id.String())
	}
	sort.Strings(labels)
	// The hash covers the source content and what it resolved to under the
	// current mapping, so changing the mapping and re-running updates issues
	// whose source did not change.
	hash := importer.TargetHash(item.hash,
		target.TeamID.String(), statusID.String(), strconv.Itoa(int(priority)), optionalID(assigneeID),
		optionalID(projectID), optionalID(item.cycleID), strings.Join(labels, ","), item.parentExternalID, unmatchedAuthor)

	key := reportKey(issue)
	existing, err := r.ref(domain.ImportExternalIssue, issue.ExternalID)
	if err != nil {
		return uuid.Nil, err
	}
	if existing != nil && existing.ContentHash != nil && *existing.ContentHash == hash {
		r.delta.Skipped++
		return existing.KuayleID, nil
	}

	// Attachments are stored before the issue so the description links only
	// files that were accepted. Already imported ones are relinked from their
	// refs, so an update or a retry after a failed create keeps every link.
	attachments, err := r.storeAttachments(item, target.TeamID)
	if err != nil {
		return uuid.Nil, err
	}
	description := r.issueBody(issue.Description, unmatchedAuthor, attachments)
	title := sanitize.PlainText(issue.Title)
	var assigneeIDs []uuid.UUID
	if assigneeID != nil {
		assigneeIDs = []uuid.UUID{*assigneeID}
	}

	var issueID uuid.UUID
	if existing != nil {
		stored, err := r.svc.store.GetIssue(r.ctx, r.job.WorkspaceID, existing.KuayleID)
		if err != nil {
			return uuid.Nil, fmt.Errorf("could not load the imported issue: %w", err)
		}
		if stored == nil {
			return uuid.Nil, errors.New("the imported issue was deleted or is no longer visible")
		}
		stored.Title = title
		stored.Description = &description
		stored.Priority = priority
		stored.AssigneeID = assigneeID
		if stored.TeamID == target.TeamID {
			stored.StatusID = &statusID
			stored.Status = issueStatus(r.categoryForStatusID(target.TeamID, statusID))
			stored.ProjectID = projectID
			stored.CycleID = item.cycleID
		} else if mapped, ok := r.mapping.ResolveStatus(stored.TeamID, issue.Status); ok && mapped.TeamID == stored.TeamID {
			// The issue was moved to another team in Kuayle. Values resolved
			// for the mapped team would point into the wrong team, so only a
			// status mapped for its current team is applied; its status
			// otherwise, and its project and cycle, stay as they are.
			stored.StatusID = &mapped.StatusID
			stored.Status = issueStatus(r.categoryForStatusID(stored.TeamID, mapped.StatusID))
		}
		if err := r.svc.store.UpdateIssue(r.ctx, r.job.WorkspaceID, stored, issue.UpdatedAt); err != nil {
			return uuid.Nil, err
		}
		issueID = stored.ID
		// IssueRepo.Update has no transaction parameter, so the ref write here
		// is separate from the issue update. A failure only leaves a stale hash:
		// the next re-run updates the issue again and does not duplicate it.
		if err := r.updateRef(existing, key, hash, issue.URL); err != nil {
			return uuid.Nil, err
		}
		r.delta.Updated++
	} else {
		created := &domain.Issue{
			Title: title, Description: &description, Status: issueStatus(r.categoryForStatusID(target.TeamID, statusID)),
			StatusID: &statusID, Priority: priority, CreatorID: creatorID, AssigneeID: assigneeID,
			ProjectID: projectID, CycleID: item.cycleID, Triaged: !r.teams[target.TeamID].TriageEnabled,
		}
		ref := r.newRef(domain.ImportExternalIssue, issue.ExternalID, key, uuid.Nil, hash, issue.URL)
		if err := r.svc.store.CreateIssue(r.ctx, r.job.WorkspaceID, target.TeamID, created, issue.CreatedAt, issue.UpdatedAt, ref); err != nil {
			return uuid.Nil, err
		}
		issueID = created.ID
		r.cacheRef(ref)
		r.delta.Created++
	}
	if len(item.labelIDs) > 0 {
		if err := r.svc.store.SetLabels(r.ctx, r.job.WorkspaceID, issueID, item.labelIDs); err != nil {
			return uuid.Nil, err
		}
	}
	// On updates we must also clear a previously assigned user when the source
	// is now unmatched/unassigned; passing nil/empty clears all assignees.
	if existing != nil || assigneeID != nil {
		if err := r.svc.store.SetAssignees(r.ctx, r.job.WorkspaceID, issueID, assigneeIDs); err != nil {
			return uuid.Nil, err
		}
	}
	r.writeComments(issue, issueID)
	return issueID, nil
}

func (r *importRun) categoryForStatusID(teamID, statusID uuid.UUID) domain.StatusCategory {
	for _, status := range r.teamStatuses(teamID) {
		if status.ID == statusID {
			return status.Category
		}
	}
	return domain.StatusCategoryUnstarted
}

// attributionHTML names an author who could not be matched to a member.
func attributionHTML(name string) string {
	if name == "" {
		return ""
	}
	return "<p><em>Originally by " + html.EscapeString(name) + "</em></p>"
}

// issueBody builds the stored description as editor HTML: the attribution,
// the source text converted from Markdown (a literal tag stays text) and the
// imported files as the editor's own attachment nodes. Attachment links go
// through the workspace asset route, which checks team visibility, never a
// storage URL.
func (r *importRun) issueBody(markdown, unmatchedAuthor string, attachments []importedAttachment) string {
	var body strings.Builder
	body.WriteString(attributionHTML(unmatchedAuthor))
	body.WriteString(sanitize.EditorMarkdown(markdown))
	if len(attachments) > 0 {
		body.WriteString("<p><strong>Attachments</strong></p>")
		for _, attachment := range attachments {
			name := html.EscapeString(attachment.filename)
			href := "/api/workspaces/" + url.PathEscape(r.slug) + "/assets/" + attachment.assetID.String() + "?download=1"
			fmt.Fprintf(&body, `<p><a class="editor-attachment" data-type="attachment" data-filename="%s" data-size="%d" href="%s" download="%s" target="_blank">%s</a></p>`,
				name, attachment.size, html.EscapeString(href), name, name)
		}
	}
	return sanitize.SanitizeHTML(body.String())
}

func attachmentReason(filename string, err error) string {
	if strings.Contains(err.Error(), filename) {
		return err.Error()
	}
	return filename + ": " + err.Error()
}

func (r *importRun) storeAttachments(item preparedIssue, teamID uuid.UUID) ([]importedAttachment, error) {
	key := reportKey(item.source)
	out := []importedAttachment{}
	for _, attachment := range item.source.Attachments {
		ref, err := r.ref(domain.ImportExternalAttachment, attachment.ExternalID)
		if err != nil {
			return nil, err
		}
		if ref != nil {
			asset, err := r.svc.store.Asset(r.ctx, r.job.WorkspaceID, ref.KuayleID)
			if err != nil {
				r.item(domain.ImportExternalAttachment, attachment.ExternalID, key, domain.ImportOutcomeSkipped, attachmentReason(attachment.Filename, err))
				continue
			}
			out = append(out, importedAttachment{assetID: asset.ID, filename: asset.Filename, size: asset.Size})
			continue
		}
		r.heartbeat(true)
		content, err := r.source.OpenAttachment(r.ctx, attachment)
		if err != nil {
			r.item(domain.ImportExternalAttachment, attachment.ExternalID, key, domain.ImportOutcomeSkipped, attachmentReason(attachment.Filename, err))
			continue
		}
		asset, err := r.svc.store.StoreAttachment(r.ctx, r.job.WorkspaceID, teamID, r.importerID, attachment.Filename, content, attachment.Size, attachment.ContentType)
		_ = content.Close()
		if err != nil {
			r.item(domain.ImportExternalAttachment, attachment.ExternalID, key, domain.ImportOutcomeSkipped, attachmentReason(attachment.Filename, err))
			continue
		}
		out = append(out, importedAttachment{assetID: asset.ID, filename: asset.Filename, size: asset.Size})
		if err := r.upsertRef(domain.ImportExternalAttachment, attachment.ExternalID, key, asset.ID, "", ""); err != nil {
			// The asset still exists, so keep its link and report the
			// persistence failure as a failed item rather than hiding it.
			r.item(domain.ImportExternalAttachment, attachment.ExternalID, key, domain.ImportOutcomeFailed, err.Error())
		}
	}
	return out, nil
}

// syncComment creates a comment, or updates the imported one in place when
// its source changed. Unchanged comments are left alone.
func (r *importRun) syncComment(issueID uuid.UUID, externalID, key string, authorID uuid.UUID, body, hash string, createdAt, updatedAt time.Time) {
	existing, err := r.ref(domain.ImportExternalComment, externalID)
	if err != nil {
		r.item(domain.ImportExternalComment, externalID, key, domain.ImportOutcomeFailed, err.Error())
		return
	}
	if existing != nil {
		if existing.ContentHash != nil && *existing.ContentHash == hash {
			return
		}
		updated, err := r.svc.store.UpdateComment(r.ctx, r.job.WorkspaceID, issueID, existing.KuayleID, authorID, body)
		if err != nil {
			r.item(domain.ImportExternalComment, externalID, key, domain.ImportOutcomeFailed, err.Error())
			return
		}
		if updated == nil {
			r.item(domain.ImportExternalComment, externalID, key, domain.ImportOutcomeSkipped, "comment was deleted or edited in Kuayle")
		}
		if err := r.updateRef(existing, key, hash, ""); err != nil {
			r.item(domain.ImportExternalComment, externalID, key, domain.ImportOutcomeFailed, err.Error())
		}
		return
	}
	created := &domain.Comment{IssueID: issueID, UserID: authorID, Body: body}
	if err := r.svc.store.CreateComment(r.ctx, r.job.WorkspaceID, created, createdAt, updatedAt); err != nil {
		r.item(domain.ImportExternalComment, externalID, key, domain.ImportOutcomeFailed, err.Error())
		return
	}
	if err := r.upsertRef(domain.ImportExternalComment, externalID, key, created.ID, hash, ""); err != nil {
		r.item(domain.ImportExternalComment, externalID, key, domain.ImportOutcomeFailed, err.Error())
	}
}

func (r *importRun) writeComments(issue importer.SourceIssue, issueID uuid.UUID) {
	key := reportKey(issue)
	for _, comment := range issue.Comments {
		authorID, unmatchedAuthor := r.mapping.ResolveUser(&comment.Author, r.members, r.importerID)
		body := sanitize.SanitizeHTML(attributionHTML(unmatchedAuthor) + sanitize.EditorMarkdown(comment.Body))
		r.syncComment(issueID, comment.ExternalID, key, authorID, body, importer.CommentHash(comment), comment.CreatedAt, comment.UpdatedAt)
	}
	if strings.TrimSpace(issue.HistoryMarkdown) == "" {
		return
	}
	// The history summary grows as the source issue changes; it is synced like
	// an edited comment rather than written once.
	history := importer.SourceComment{Body: issue.HistoryMarkdown}
	r.syncComment(issueID, "history:"+issue.ExternalID, key, r.importerID, sanitize.EditorMarkdown(issue.HistoryMarkdown),
		importer.CommentHash(history), issue.UpdatedAt, issue.UpdatedAt)
}

func (r *importRun) writeRelation(work relationWork) error {
	rel := work.rel
	existing, err := r.ref(domain.ImportExternalRelation, rel.ExternalID)
	if err != nil {
		return err
	}
	if existing != nil {
		return nil
	}
	source, err := r.ref(domain.ImportExternalIssue, rel.SourceExternalID)
	if err != nil {
		return err
	}
	target, err := r.ref(domain.ImportExternalIssue, rel.TargetExternalID)
	if err != nil {
		return err
	}
	if source == nil || target == nil {
		r.item(domain.ImportExternalRelation, rel.ExternalID, work.issueKey, domain.ImportOutcomeSkipped, "linked issue was not imported")
		return nil
	}
	id, err := r.svc.store.CreateRelation(r.ctx, r.job.WorkspaceID, source.KuayleID, target.KuayleID, rel.Type)
	if err != nil {
		return err
	}
	return r.upsertRef(domain.ImportExternalRelation, rel.ExternalID, work.issueKey, id, "", "")
}

// ref returns the stored mapping for a source entity, or nil when it was never
// imported. A lookup error is returned, never treated as "not found", so the
// caller fails the item instead of creating a duplicate.
func (r *importRun) ref(extType, extID string) (*domain.ImportExternalRef, error) {
	key := refKey(extType, extID)
	if ref, ok := r.refs[key]; ok {
		return ref, nil
	}
	ref, err := r.svc.repo.GetRef(r.ctx, r.job.WorkspaceID, r.job.Source, r.job.SourceSite, extType, extID)
	if err != nil {
		return nil, err
	}
	if ref == nil {
		return nil, nil
	}
	r.refs[key] = ref
	return ref, nil
}

// newRef builds an external ref. key is the source's human key (PROJ-123) of
// the issue the entity belongs to, kept as the reference to the original.
func (r *importRun) newRef(extType, externalID, key string, kuayleID uuid.UUID, hash, sourceURL string) *domain.ImportExternalRef {
	var hashPtr *string
	if hash != "" {
		hashPtr = &hash
	}
	var urlPtr *string
	if sourceURL != "" {
		urlPtr = &sourceURL
	}
	jobID := r.job.ID
	return &domain.ImportExternalRef{
		WorkspaceID: r.job.WorkspaceID, Source: r.job.Source, SourceSite: r.job.SourceSite,
		ExternalType: extType, ExternalID: externalID, ExternalKey: key, KuayleID: kuayleID,
		ContentHash: hashPtr, ExternalURL: urlPtr, LastJobID: &jobID,
	}
}

func (r *importRun) cacheRef(ref *domain.ImportExternalRef) {
	r.refs[refKey(ref.ExternalType, ref.ExternalID)] = ref
}

func (r *importRun) upsertRef(extType, externalID, key string, kuayleID uuid.UUID, hash, sourceURL string) error {
	ref := r.newRef(extType, externalID, key, kuayleID, hash, sourceURL)
	if err := r.svc.repo.UpsertRef(r.ctx, nil, ref); err != nil {
		return err
	}
	r.cacheRef(ref)
	return nil
}

func (r *importRun) updateRef(ref *domain.ImportExternalRef, key, hash, sourceURL string) error {
	ref.ExternalKey = key
	ref.ContentHash = &hash
	if sourceURL != "" {
		ref.ExternalURL = &sourceURL
	}
	jobID := r.job.ID
	ref.LastJobID = &jobID
	return r.svc.repo.UpsertRef(r.ctx, nil, ref)
}

func (r *importRun) tick() error {
	r.delta.Processed++
	r.sinceFlush++
	if r.sinceFlush < 10 {
		r.heartbeat(false)
		return nil
	}
	r.sinceFlush = 0
	if err := r.flush(); err != nil {
		return err
	}
	cancelled, err := r.svc.repo.IsCancelRequested(r.ctx, r.job.ID)
	if err != nil {
		return err
	}
	if cancelled {
		return ErrImportCancelled
	}
	return nil
}

func (r *importRun) flush() error {
	delta := r.delta
	if delta.Processed == 0 && delta.Created == 0 && delta.Updated == 0 && delta.Skipped == 0 && delta.Failed == 0 {
		return nil
	}
	if err := r.svc.repo.IncrementCounters(r.ctx, r.job.ID, delta); err != nil {
		return err
	}
	r.delta = repository.ImportCounterDelta{}
	return nil
}

// item records one row of the per-item report. key is the source's human key
// of the issue the item belongs to.
func (r *importRun) item(extType, externalID, key, outcome, reason string) {
	item := &domain.ImportJobItem{
		JobID: r.job.ID, WorkspaceID: r.job.WorkspaceID, ExternalType: extType,
		ExternalID: externalID, ExternalKey: key, Outcome: outcome, Reason: reason,
	}
	if err := r.svc.repo.AddItem(r.ctx, item); err != nil {
		r.svc.logger.WithError(err).Warn("could not record import item")
	}
	// Progress counters describe issues only. Comment, attachment, relation
	// and parent-link outcomes still appear in the per-item report, but they
	// must not move the issue counters.
	if extType != domain.ImportExternalIssue {
		return
	}
	if outcome == domain.ImportOutcomeFailed {
		r.delta.Failed++
	} else {
		r.delta.Skipped++
	}
}

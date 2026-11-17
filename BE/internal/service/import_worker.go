package service

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/kuayle/kuayle-backend/internal/domain"
	"github.com/kuayle/kuayle-backend/internal/importer"
	"github.com/kuayle/kuayle-backend/internal/repository"
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
	importerID    uuid.UUID
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

type relationWork struct {
	issueID uuid.UUID
	rel     importer.SourceRelation
}

func refKey(extType, extID string) string { return extType + "\x00" + extID }

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
	members, err := s.store.Members(workCtx, job.WorkspaceID)
	if err != nil {
		finish(domain.ImportStatusFailed, err.Error())
		return
	}
	run := &importRun{
		svc: s, ctx: workCtx, job: job, source: source, mapping: mapping, members: members,
		importerID: *job.CreatedBy, statuses: map[uuid.UUID][]domain.TeamStatus{}, refs: map[string]*domain.ImportExternalRef{},
	}
	if err := run.execute(); err != nil {
		if errors.Is(err, ErrImportCancelled) {
			_ = run.flush()
			finish(domain.ImportStatusCancelled, "")
		} else {
			_ = run.flush()
			finish(domain.ImportStatusFailed, err.Error())
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
	if err := r.source.Fetch(r.ctx, func(issue importer.SourceIssue) error {
		issues = append(issues, issue)
		r.heartbeat(false)
		return nil
	}); err != nil {
		return fmt.Errorf("fetch source: %w", err)
	}
	prepared := r.prepare(issues)

	parents := map[uuid.UUID]string{}
	relations := []relationWork{}
	for _, item := range prepared {
		if err := r.tick(); err != nil {
			return err
		}
		if item.prepErr != nil {
			r.item(domain.ImportExternalIssue, item.source.ExternalID,
				importer.ExternalKey(r.job.Source, r.job.SourceSite, domain.ImportExternalIssue, item.source.ExternalID),
				domain.ImportOutcomeFailed, item.prepErr.Error())
			continue
		}
		issueID, parent, links, err := r.writeIssue(item)
		if err != nil {
			r.item(domain.ImportExternalIssue, item.source.ExternalID, importer.ExternalKey(r.job.Source, r.job.SourceSite, domain.ImportExternalIssue, item.source.ExternalID), domain.ImportOutcomeFailed, err.Error())
			continue
		}
		if parent != "" && issueID != uuid.Nil {
			parents[issueID] = parent
		}
		for _, link := range links {
			relations = append(relations, relationWork{issueID: issueID, rel: link})
		}
	}

	for issueID, externalID := range parents {
		target, err := r.ref(refKey(domain.ImportExternalIssue, externalID))
		if err != nil {
			r.item(domain.ImportExternalIssue, externalID, "", domain.ImportOutcomeFailed, err.Error())
			continue
		}
		if target == nil {
			r.item(domain.ImportExternalIssue, externalID, "", domain.ImportOutcomeSkipped, "parent issue was not imported")
			continue
		}
		if err := r.svc.store.SetParent(r.ctx, r.job.WorkspaceID, issueID, target.KuayleID, r.importerID); err != nil {
			r.item(domain.ImportExternalIssue, externalID, "", domain.ImportOutcomeFailed, err.Error())
		}
	}

	for _, work := range relations {
		if err := r.writeRelation(work); err != nil {
			r.item(domain.ImportExternalRelation, work.rel.ExternalID, "", domain.ImportOutcomeFailed, err.Error())
		}
	}
	return nil
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
			result, found := labelCache[name]
			if !found {
				result.value, result.err = r.svc.store.EnsureLabel(r.ctx, r.job.WorkspaceID, nil, name)
				labelCache[name] = result
			}
			if result.err != nil {
				prepared.prepErr = result.err
				break
			}
			prepared.labelIDs = append(prepared.labelIDs, result.value)
		}
		if prepared.prepErr == nil && r.mapping.SprintMode != importer.SprintModeIgnore {
			for _, name := range issue.SprintIDs {
				key := cycleKey{teamID: prepared.teamID, name: name}
				result, found := cycleCache[key]
				if !found {
					result.value, result.err = r.svc.store.EnsureCycle(r.ctx, r.job.WorkspaceID, prepared.teamID, name)
					cycleCache[key] = result
				}
				if result.err != nil {
					prepared.prepErr = result.err
					break
				}
				prepared.cycleID = &result.value
				break
			}
		}
		if prepared.prepErr == nil && issue.EpicExternalID != "" {
			switch r.mapping.EpicMode {
			case importer.EpicModeParent:
				if prepared.parentExternalID == "" {
					prepared.parentExternalID = issue.EpicExternalID
				}
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

func (r *importRun) statusForTeam(teamID uuid.UUID) (domain.TeamStatus, bool) {
	statuses, ok := r.statuses[teamID]
	if !ok {
		list, err := r.svc.store.Statuses(r.ctx, r.job.WorkspaceID, teamID)
		if err != nil {
			return domain.TeamStatus{}, false
		}
		statuses = list
		r.statuses[teamID] = list
	}
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

func (r *importRun) writeIssue(item preparedIssue) (uuid.UUID, string, []importer.SourceRelation, error) {
	issue := item.source
	target, ok := r.mapping.Projects[issue.ProjectExternalID]
	if !ok || target.TeamID == uuid.Nil {
		return uuid.Nil, "", nil, importer.ErrNoProjectMapping
	}
	statusID := uuid.Nil
	status, mapped := r.mapping.ResolveStatus(issue.ProjectExternalID, issue.Status)
	if mapped {
		statusID = status.StatusID
	} else if fallback, found := r.statusForTeam(target.TeamID); found {
		statusID = fallback.ID
		status.TeamID = target.TeamID
	} else {
		return uuid.Nil, "", nil, fmt.Errorf("no status available for team %s", target.TeamID)
	}
	priority, _ := r.mapping.ResolvePriority(issue.Priority)
	creatorID, attribution := r.mapping.ResolveUser(issue.Reporter, r.members, r.importerID)
	// Assignees resolve differently from authors: a missing, unmatched or
	// explicitly unassigned source user leaves the issue with no assignee.
	assigneeID := r.mapping.ResolveAssignee(issue.Assignee, r.members)
	description := issue.Description
	if attribution != "" {
		description = attribution + "\n\n" + description
	}

	key := importer.ExternalKey(r.job.Source, r.job.SourceSite, domain.ImportExternalIssue, issue.ExternalID)
	if err := r.storeAttachments(item, target.TeamID, &description); err != nil {
		return uuid.Nil, "", nil, err
	}
	category := r.categoryForStatusID(target.TeamID, statusID)

	existing, err := r.ref(refKey(domain.ImportExternalIssue, issue.ExternalID))
	if err != nil {
		return uuid.Nil, "", nil, err
	}
	if existing != nil && existing.ContentHash != nil && *existing.ContentHash == item.hash {
		r.delta.Skipped++
		return existing.KuayleID, item.parentExternalID, issue.Relations, nil
	}

	projectID := target.ProjectID
	if item.projectOverride != nil {
		projectID = item.projectOverride
	}
	assignee := assigneeID
	var assigneeIDs []uuid.UUID
	if assigneeID != nil {
		assigneeIDs = []uuid.UUID{*assigneeID}
	}
	var issueID uuid.UUID
	if existing != nil {
		stored, err := r.svc.store.GetIssue(r.ctx, r.job.WorkspaceID, existing.KuayleID)
		if err != nil || stored == nil {
			return uuid.Nil, "", nil, fmt.Errorf("could not load imported issue: %v", err)
		}
		stored.Title = issue.Title
		stored.Description = &description
		stored.Status = issueStatus(category)
		stored.StatusID = &statusID
		stored.Priority = priority
		stored.AssigneeID = assignee
		stored.ProjectID = projectID
		stored.CycleID = item.cycleID
		if err := r.svc.store.UpdateIssue(r.ctx, r.job.WorkspaceID, stored, issue.UpdatedAt); err != nil {
			return uuid.Nil, "", nil, err
		}
		issueID = stored.ID
		// IssueRepo.Update has no transaction parameter, so the ref write here
		// is separate from the issue update. A failure only leaves a stale hash:
		// the next re-run updates the issue again and does not duplicate it.
		if err := r.updateRef(existing, item.hash, issue.URL); err != nil {
			return uuid.Nil, "", nil, err
		}
		r.delta.Updated++
	} else {
		created := &domain.Issue{
			Title: issue.Title, Description: &description, Status: issueStatus(category),
			StatusID: &statusID, Priority: priority, CreatorID: creatorID, AssigneeID: assignee,
			ProjectID: projectID, CycleID: item.cycleID,
		}
		ref := r.newRef(domain.ImportExternalIssue, issue.ExternalID, key, uuid.Nil, item.hash, issue.URL)
		if err := r.svc.store.CreateIssue(r.ctx, r.job.WorkspaceID, target.TeamID, created, issue.CreatedAt, issue.UpdatedAt, ref); err != nil {
			return uuid.Nil, "", nil, err
		}
		issueID = created.ID
		r.cacheRef(ref)
		r.delta.Created++
	}
	if len(item.labelIDs) > 0 {
		if err := r.svc.store.SetLabels(r.ctx, r.job.WorkspaceID, issueID, item.labelIDs); err != nil {
			return uuid.Nil, "", nil, err
		}
	}
	// On updates we must also clear a previously assigned user when the source
	// is now unmatched/unassigned; passing nil/empty clears all assignees.
	if existing != nil || assigneeID != nil {
		if err := r.svc.store.SetAssignees(r.ctx, r.job.WorkspaceID, issueID, assigneeIDs); err != nil {
			return uuid.Nil, "", nil, err
		}
	}
	r.writeComments(issue, issueID)
	return issueID, item.parentExternalID, issue.Relations, nil
}

func (r *importRun) categoryForStatusID(teamID, statusID uuid.UUID) domain.StatusCategory {
	statuses := r.statuses[teamID]
	if statuses == nil {
		list, err := r.svc.store.Statuses(r.ctx, r.job.WorkspaceID, teamID)
		if err != nil {
			return domain.StatusCategoryUnstarted
		}
		statuses = list
		r.statuses[teamID] = list
	}
	for _, status := range statuses {
		if status.ID == statusID {
			return status.Category
		}
	}
	return domain.StatusCategoryUnstarted
}

func (r *importRun) storeAttachments(item preparedIssue, teamID uuid.UUID, description *string) error {
	// Attachments are stored before the issue so the description can link only
	// to files that were actually accepted; oversized files are reported and
	// skipped without failing the job. Links for already-imported attachments
	// are rebuilt from their refs so re-runs never drop them.
	links := []string{}
	for _, attachment := range item.source.Attachments {
		key := importer.ExternalKey(r.job.Source, r.job.SourceSite, domain.ImportExternalAttachment, attachment.ExternalID)
		ref, err := r.ref(refKey(domain.ImportExternalAttachment, attachment.ExternalID))
		if err != nil {
			return err
		}
		var filename, url string
		if ref != nil {
			filename, url, err = r.svc.store.AssetURL(r.ctx, r.job.WorkspaceID, ref.KuayleID)
			if err != nil {
				r.item(domain.ImportExternalAttachment, attachment.ExternalID, key, domain.ImportOutcomeSkipped, err.Error())
				continue
			}
		} else {
			r.heartbeat(true)
			content, err := r.source.OpenAttachment(r.ctx, attachment)
			if err != nil {
				r.item(domain.ImportExternalAttachment, attachment.ExternalID, key, domain.ImportOutcomeSkipped, err.Error())
				continue
			}
			asset, storedURL, err := r.svc.store.StoreAttachment(r.ctx, r.job.WorkspaceID, teamID, r.importerID, attachment.Filename, content, attachment.Size, attachment.ContentType)
			_ = content.Close()
			if err != nil {
				r.item(domain.ImportExternalAttachment, attachment.ExternalID, key, domain.ImportOutcomeSkipped, err.Error())
				continue
			}
			filename, url = attachment.Filename, storedURL
			if err := r.upsertRef(domain.ImportExternalAttachment, attachment.ExternalID, key, asset.ID, "", ""); err != nil {
				// The asset still exists, so keep its link and report the
				// persistence failure as a failed item rather than hiding it.
				r.item(domain.ImportExternalAttachment, attachment.ExternalID, key, domain.ImportOutcomeFailed, err.Error())
			}
		}
		if url != "" {
			links = append(links, fmt.Sprintf("- [%s](%s)", filename, url))
		} else {
			links = append(links, fmt.Sprintf("- %s", filename))
		}
	}
	if len(links) > 0 {
		*description += "\n\n**Attachments**\n" + strings.Join(links, "\n")
	}
	return nil
}

func (r *importRun) writeComments(issue importer.SourceIssue, issueID uuid.UUID) {
	for _, comment := range issue.Comments {
		key := importer.ExternalKey(r.job.Source, r.job.SourceSite, domain.ImportExternalComment, comment.ExternalID)
		existing, err := r.ref(refKey(domain.ImportExternalComment, comment.ExternalID))
		if err != nil {
			r.item(domain.ImportExternalComment, comment.ExternalID, key, domain.ImportOutcomeFailed, err.Error())
			continue
		}
		if existing != nil {
			continue
		}
		authorID, prefix := r.mapping.ResolveUser(&comment.Author, r.members, r.importerID)
		body := comment.Body
		if prefix != "" {
			body = prefix + "\n\n" + body
		}
		created := &domain.Comment{IssueID: issueID, UserID: authorID, Body: body}
		if err := r.svc.store.CreateComment(r.ctx, r.job.WorkspaceID, created, comment.CreatedAt, comment.UpdatedAt); err != nil {
			r.item(domain.ImportExternalComment, comment.ExternalID, key, domain.ImportOutcomeFailed, err.Error())
			continue
		}
		if err := r.upsertRef(domain.ImportExternalComment, comment.ExternalID, key, created.ID, "", ""); err != nil {
			r.item(domain.ImportExternalComment, comment.ExternalID, key, domain.ImportOutcomeFailed, err.Error())
		}
	}
	if strings.TrimSpace(issue.HistoryMarkdown) == "" {
		return
	}
	key := importer.ExternalKey(r.job.Source, r.job.SourceSite, domain.ImportExternalComment, "history:"+issue.ExternalID)
	existing, err := r.ref(refKey(domain.ImportExternalComment, "history:"+issue.ExternalID))
	if err != nil {
		r.item(domain.ImportExternalComment, "history:"+issue.ExternalID, key, domain.ImportOutcomeFailed, err.Error())
		return
	}
	if existing != nil {
		return
	}
	history := &domain.Comment{IssueID: issueID, UserID: r.importerID, Body: issue.HistoryMarkdown}
	if err := r.svc.store.CreateComment(r.ctx, r.job.WorkspaceID, history, issue.UpdatedAt, issue.UpdatedAt); err != nil {
		r.item(domain.ImportExternalComment, "history:"+issue.ExternalID, key, domain.ImportOutcomeFailed, err.Error())
		return
	}
	if err := r.upsertRef(domain.ImportExternalComment, "history:"+issue.ExternalID, key, history.ID, "", ""); err != nil {
		r.item(domain.ImportExternalComment, "history:"+issue.ExternalID, key, domain.ImportOutcomeFailed, err.Error())
	}
}

func (r *importRun) writeRelation(work relationWork) error {
	rel := work.rel
	key := importer.ExternalKey(r.job.Source, r.job.SourceSite, domain.ImportExternalRelation, rel.ExternalID)
	existing, err := r.ref(refKey(domain.ImportExternalRelation, rel.ExternalID))
	if err != nil {
		return err
	}
	if existing != nil {
		return nil
	}
	source, err := r.ref(refKey(domain.ImportExternalIssue, rel.SourceExternalID))
	if err != nil {
		return err
	}
	target, err := r.ref(refKey(domain.ImportExternalIssue, rel.TargetExternalID))
	if err != nil {
		return err
	}
	if source == nil || target == nil {
		r.item(domain.ImportExternalRelation, rel.ExternalID, key, domain.ImportOutcomeSkipped, "linked issue was not imported")
		return nil
	}
	id, err := r.svc.store.CreateRelation(r.ctx, r.job.WorkspaceID, source.KuayleID, target.KuayleID, rel.Type)
	if err != nil {
		return err
	}
	return r.upsertRef(domain.ImportExternalRelation, rel.ExternalID, key, id, "", "")
}

func (r *importRun) ref(key string) (*domain.ImportExternalRef, error) {
	if ref, ok := r.refs[key]; ok {
		return ref, nil
	}
	parts := strings.SplitN(key, "\x00", 2)
	if len(parts) != 2 {
		return nil, nil
	}
	ref, err := r.svc.repo.GetRef(r.ctx, r.job.WorkspaceID, r.job.Source, r.job.SourceSite, parts[0], parts[1])
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return nil, err
	}
	if err != nil || ref == nil {
		return nil, nil
	}
	r.refs[key] = ref
	return ref, nil
}

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

func (r *importRun) updateRef(ref *domain.ImportExternalRef, hash, sourceURL string) error {
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

func (r *importRun) item(extType, externalID, key, outcome, reason string) {
	if key == "" {
		key = importer.ExternalKey(r.job.Source, r.job.SourceSite, extType, externalID)
	}
	item := &domain.ImportJobItem{
		JobID: r.job.ID, WorkspaceID: r.job.WorkspaceID, ExternalType: extType,
		ExternalID: externalID, ExternalKey: key, Outcome: outcome, Reason: reason,
	}
	if err := r.svc.repo.AddItem(r.ctx, item); err != nil {
		r.svc.logger.WithError(err).Warn("could not record import item")
	}
	// Progress counters describe issues only. Comment, attachment and
	// relation outcomes still appear here in the per-item report, but they
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

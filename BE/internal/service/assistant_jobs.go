package service

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/kuayle/kuayle-backend/internal/domain"
	"github.com/kuayle/kuayle-backend/internal/repository"
	log "github.com/sirupsen/logrus"
)

var assistantJobTools = []assistantTool{
	{name: "schedule_job", jobs: true, description: "Register a reminder or a later assistant turn in this chat only on explicit request. Use once with at (local ISO date/time or RFC3339) or delay_seconds; interval with interval_seconds; cron with five fields in profile time; condition with interval_seconds, deadline and a read tool probe. Use dot paths (array indices allowed) into the tool result. Notify posts instructions verbatim; continue uses them as a new turn with normal approvals. Report registration and next run.", parameters: `{"type":"object","properties":{"kind":{"enum":["once","interval","cron","condition"]},"action":{"enum":["notify","continue"]},"instructions":{"type":"string","minLength":1,"maxLength":2000},"at":{"type":"string"},"delay_seconds":{"type":"integer","minimum":1,"maximum":2592000},"interval_seconds":{"type":"integer","minimum":60,"maximum":2592000},"cron":{"type":"string","maxLength":128},"deadline":{"type":"string"},"condition":{"type":"object","properties":{"tool":{"type":"string"},"arguments":{"type":"object"},"path":{"type":"string","minLength":1,"maxLength":256},"op":{"enum":["eq","ne","gt","gte","lt","lte"]},"value":{"type":["string","boolean","number"]}},"required":["tool","arguments","path","op","value"],"additionalProperties":false}},"required":["kind","action","instructions"],"additionalProperties":false}`},
	{name: "list_scheduled_jobs", jobs: true, description: "List this conversation's scheduled jobs and recent outcomes.", parameters: `{"type":"object","properties":{},"additionalProperties":false}`},
	{name: "cancel_scheduled_job", jobs: true, description: "Cancel a job in this conversation on the user's request.", parameters: `{"type":"object","properties":{"id":{"type":"string","format":"uuid"}},"required":["id"],"additionalProperties":false}`},
}

// SetScheduledJobs enables scheduling once the assistant tools and notifications are wired.
func (s *ConversationService) SetScheduledJobs(n notificationCreator, hub userEventPublisher) {
	s.tools.jobs = true
	s.jobNotifications, s.jobEvents = n, hub
}

// A probe uses an explicit read-only catalog because some ordinary tools without a write flag (e.g. stop_preview)
// still change the world. New tools must be reviewed before use unattended.
func scheduledRead(name string) bool {
	switch name {
	case "get_instance_version", "repo_list_files", "repo_glob", "repo_grep", "repo_read_file", "repo_symbols",
		"list_ports", "get_task_worktree", "list_machine_worktrees", getMachineUsageTool, getMachineLogsTool,
		"web_search", "get_issue", "search_issues", "list_teams", "get_agent_work", "list_agent_work",
		"get_pull_request", "get_pull_request_checks", "list_pull_request_comments",
		"get_release", "list_releases", "get_workflow_run", "draft_release_notes", "get_command_run",
		"get_wiki_page", "list_wiki_pages", "list_wiki_page_versions", "get_task_messages", "plan_issues":
		return true
	}
	return false
}

func (s *ConversationService) scheduledToolAllowed(name string) bool {
	if s.tools.customToolCall(name) {
		return true
	}
	tool, ok := lookupAssistantTool(name)
	if !ok || !s.tools.available(tool) {
		return false
	}
	if tool.approval {
		return true
	}
	return !tool.write && (scheduledRead(name) || name == "get_tool_run" || name == "load_skill" || name == "get_process")
}

func (s *ConversationService) scheduledJobTool(ctx context.Context, a domain.ConversationAttempt, callID uuid.UUID, name, arguments string) (any, *uuid.UUID, error) {
	if !s.tools.jobs {
		return nil, nil, toolFailure("unknown tool")
	}
	c := a.Conversation
	switch name {
	case "list_scheduled_jobs":
		jobs, err := s.Repo.ListScheduledJobs(ctx, c.WorkspaceID, c.ID)
		for i := range jobs {
			jobs[i].Instructions = truncateRunes(jobs[i].Instructions, 160)
			jobs[i].LastError = truncateRunes(jobs[i].LastError, 100)
		}
		return jobs, nil, err
	case "cancel_scheduled_job":
		var in struct {
			ID uuid.UUID `json:"id"`
		}
		if err := decodeToolInput(arguments, &in); err != nil {
			return nil, nil, err
		}
		job, err := s.CancelScheduledJob(ctx, c.WorkspaceID, c.ID, in.ID)
		return job, nil, err
	}
	var spec jobSpec
	if err := decodeToolInput(arguments, &spec); err != nil {
		return nil, nil, err
	}
	spec.Instructions = strings.TrimSpace(spec.Instructions)
	if spec.Instructions == "" {
		return nil, nil, toolFailure("give a reminder or instructions for the later turn")
	}
	zone := assistantTimezone(s.profileTimezone(ctx, c.WorkspaceID, c.CreatorID), time.UTC)
	next, deadline, err := spec.schedule(s.clock(), zone)
	if err != nil {
		return nil, nil, err
	}
	if probe := spec.Condition; probe != nil {
		if err = s.tools.refuseDisabled(ctx, c.WorkspaceID, probe.Tool); err != nil {
			return nil, nil, err
		}
		if s.tools.customToolCall(probe.Tool) {
			prepared, e := s.prepareJobCustomTool(ctx, c.WorkspaceID, c.CreatorID, *probe)
			if e != nil {
				return nil, nil, e
			}
			if !prepared.tool.ReadOnly || prepared.tool.RunMode != domain.AssistantToolRunAuto {
				return s.requestCustomToolApproval(ctx, a, callID, prepared)
			}
		} else {
			tool, ok := lookupAssistantTool(probe.Tool)
			if !ok || !s.tools.available(tool) {
				return nil, nil, toolFailure("probe tool is unavailable")
			}
			if err = tool.validateArguments(string(probe.Arguments)); err != nil {
				return nil, nil, err
			}
			if tool.approval {
				return s.requestApproval(ctx, a, callID, tool.name, string(probe.Arguments))
			}
			if tool.write || !scheduledRead(tool.name) {
				return nil, nil, toolFailure("this tool cannot run as a read-only probe; use a read tool or request the action interactively")
			}
		}
	}
	data, err := json.Marshal(spec)
	if err != nil {
		return nil, nil, err
	}
	j, err := s.Repo.CreateScheduledJob(ctx, a, domain.ConversationJob{Timezone: zone.String(), Kind: spec.Kind, Action: spec.Action, Spec: data, Instructions: spec.Instructions, NextRunAt: next, Deadline: deadline})
	if errors.Is(err, repository.ErrScheduledJobLimit) {
		return nil, nil, toolFailure("active job limit reached; cancel an existing job first")
	}
	return j, nil, err
}

func (s *ConversationService) CancelScheduledJob(ctx context.Context, ws, c, id uuid.UUID) (*domain.ConversationJob, error) {
	j, changed, err := s.Repo.CancelScheduledJob(ctx, ws, c, id)
	if changed {
		auditJob(*j, "cancelled")
		s.notifyJob(ctx, *j, "Scheduled job cancelled: "+j.Instructions)
	}
	return j, err
}

func auditJob(j domain.ConversationJob, outcome string) {
	auditAssistantAction("scheduled_job", j.UserID, j.WorkspaceID, "schedule_job", map[string]any{"job_id": j.ID, "conversation_id": j.ConversationID, "outcome": outcome, "attempt": j.Attempts})
}

func (s *ConversationService) RunScheduledJobs(ctx context.Context) {
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()
	for {
		for range 10 {
			j, err := s.Repo.ClaimScheduledJob(ctx, time.Minute)
			if err != nil {
				if ctx.Err() == nil {
					log.WithError(err).Warn("Could not claim a scheduled assistant job")
				}
				break
			}
			if j == nil {
				break
			}
			auditJob(*j, "claimed")
			pass, stop := context.WithTimeout(ctx, 45*time.Second)
			err = s.deliverJob(pass, *j)
			stop()
			if err != nil {
				auditJob(*j, "claim unfinished")
				log.WithField("job_id", j.ID).WithError(err).Warn("Scheduled assistant job claim did not finish")
			}
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func (s *ConversationService) deliverJob(ctx context.Context, j domain.ConversationJob) error {
	ctx = domain.WithAccess(ctx, domain.Access{WorkspaceID: j.WorkspaceID, UserID: j.UserID})
	// Authorize both before external reads and inside the transaction which posts.
	err := s.Repo.Authorize(ctx, j.WorkspaceID, j.ConversationID)
	if errors.Is(err, repository.ErrConversationNotFound) {
		auditJob(j, "access removed")
		return s.Repo.RetireInaccessibleJob(ctx, j)
	}
	if err != nil {
		return err
	}
	o := s.jobOutcome(ctx, j)
	saved, err := s.Repo.FinishScheduledJob(ctx, j, o)
	if errors.Is(err, repository.ErrConversationBusy) {
		saved, err = s.Repo.FinishScheduledJob(ctx, j, repository.JobOutcome{State: "scheduled", Summary: "Waiting for the current reply", ConditionMet: o.ConditionMet, Next: s.clock().Add(5 * time.Second)})
		o.Fired = false
	}
	if err != nil {
		return err
	}
	auditJob(*saved, saved.LastOutcome)
	if o.Fired || saved.State == "expired" {
		title := saved.LastOutcome + ": " + saved.Instructions
		if o.Fired && o.Model == "" && saved.Action == "notify" && saved.State != "expired" {
			title = saved.Instructions
		}
		s.notifyJob(ctx, *saved, title)
	}
	return nil
}

func (s *ConversationService) jobOutcome(ctx context.Context, j domain.ConversationJob) repository.JobOutcome {
	now := s.clock()
	expire := func(why string) repository.JobOutcome {
		return repository.JobOutcome{State: "expired", Summary: why, Notice: why + ": " + j.Instructions}
	}
	if !now.Before(j.Deadline) {
		return expire("Scheduled job expired")
	}
	var spec jobSpec
	if json.Unmarshal(j.Spec, &spec) != nil {
		return expire("Invalid schedule")
	}
	interval := time.Duration(spec.IntervalSeconds) * time.Second
	retry := func(err error) repository.JobOutcome {
		base := max(interval, time.Minute)
		backoff := min(base*time.Duration(1<<min(max(j.Attempts-1, 0), 6)), max(base, time.Hour))
		return repository.JobOutcome{State: "scheduled", Summary: "Retry scheduled", Error: toolErrorText(err), Next: now.Add(backoff)}
	}
	disabled, err := s.tools.disabledTools(ctx, j.WorkspaceID)
	if err != nil {
		return retry(err)
	}
	if disabled["schedule_job"] {
		return expire("Scheduling disabled")
	}
	var approvalProbe json.RawMessage
	conditionMet := j.ConditionMet
	if probe := spec.Condition; probe != nil && !conditionMet {
		match, runID, err := s.probeJob(ctx, j, *probe)
		switch {
		case errors.Is(err, errProbeApproval):
			approvalProbe, _ = json.Marshal(probe)
		case err != nil:
			o := retry(err)
			o.ProbeRunID = runID
			return o
		case !match:
			return repository.JobOutcome{State: "scheduled", Summary: "Waiting for condition", Next: now.Add(interval), ProbeRunID: runID}
		default:
			conditionMet = true
		}
	}
	o := repository.JobOutcome{State: "completed", Summary: "Reminder sent", Notice: j.Instructions, Fired: true, ConditionMet: conditionMet}
	if j.Action == "continue" || approvalProbe != nil {
		settings, err := s.configuration(ctx, j.WorkspaceID)
		if err != nil {
			o := retry(toolFailure("workspace AI configuration unavailable"))
			o.ConditionMet = conditionMet
			return o
		}
		o.Notice, o.Provider, o.Model, o.Summary = "", settings.Provider, settings.Model, "Assistant turn queued"
		if approvalProbe != nil {
			o.ApprovalProbe = approvalProbe
			o.Summary = "Probe approval turn queued"
			return o
		}
	}
	switch j.Kind {
	case "interval":
		o.State, o.Next = "scheduled", now.Add(interval)
	case "cron":
		zone, err := time.LoadLocation(j.Timezone)
		if err != nil {
			return expire("Invalid schedule timezone")
		}
		next, err := spec.nextCron(now, zone)
		if err != nil {
			return expire("Invalid schedule")
		}
		o.State, o.Next = "scheduled", next
	}
	if o.State == "scheduled" && j.Action == "continue" && j.FireCount+1 >= maxJobContinuations {
		o.State, o.Summary = "completed", "Final assistant turn queued (job limit reached)"
	}
	return o
}

var errProbeApproval = errors.New("probe requires approval")

func (s *ConversationService) prepareJobCustomTool(ctx context.Context, ws, user uuid.UUID, c jobCondition) (*preparedToolRun, error) {
	if err := s.refuseRequest(ctx, ws, user, "run_custom_tool"); err != nil {
		return nil, err
	}
	return s.tools.prepareCustomTool(ctx, ws, user, strings.TrimPrefix(c.Tool, domain.CustomToolPrefix), string(c.Arguments))
}

func (s *ConversationService) probeJob(ctx context.Context, j domain.ConversationJob, c jobCondition) (bool, *uuid.UUID, error) {
	if err := s.tools.refuseDisabled(ctx, j.WorkspaceID, c.Tool); err != nil {
		return false, nil, err
	}
	var output any
	if s.tools.customToolCall(c.Tool) {
		prepared, err := s.prepareJobCustomTool(ctx, j.WorkspaceID, j.UserID, c)
		if err != nil {
			return false, nil, err
		}
		if !prepared.tool.ReadOnly || prepared.tool.RunMode != domain.AssistantToolRunAuto {
			return false, nil, errProbeApproval
		}
		var run *domain.AssistantToolRun
		if j.ProbeRunID != nil {
			run, err = s.tools.customTools.Run(ctx, j.WorkspaceID, *j.ProbeRunID, j.UserID)
		} else {
			run, err = s.tools.queuePreparedTool(ctx, j.WorkspaceID, j.UserID, j.ConversationID, nil, prepared)
		}
		if err != nil {
			return false, j.ProbeRunID, err
		}
		if run.Status == "running" || run.Status == "queued" {
			return false, &run.ID, nil
		}
		if run.Status != "succeeded" || run.ToolVersion != prepared.tool.CurrentVersion {
			return false, nil, toolFailure("custom probe failed or changed")
		}
		output = toolRunResult(run)
	} else {
		tool, ok := lookupAssistantTool(c.Tool)
		if !ok || !s.tools.available(tool) {
			return false, nil, toolFailure("read-only probe unavailable")
		}
		if tool.approval {
			return false, nil, errProbeApproval
		}
		if tool.write || !scheduledRead(c.Tool) {
			return false, nil, toolFailure("read-only probe unavailable")
		}
		if gap := s.readOnlyGitHubGap(ctx, j.WorkspaceID, tool); gap != nil {
			return false, nil, toolFailure("%s", gap.Message)
		}
		var err error
		output, _, err = s.tools.run(ctx, j.WorkspaceID, j.UserID, c.Tool, string(c.Arguments))
		if err != nil {
			return false, nil, err
		}
	}
	match, err := c.matches(output)
	return match, nil, err
}

func (s *ConversationService) notifyJob(ctx context.Context, j domain.ConversationJob, title string) {
	if s.jobNotifications == nil || s.jobEvents == nil {
		return
	}
	event := notificationEvent{Type: "scheduled_job", Title: truncateRunes(title, 350), IssueID: j.IssueID, Extra: map[string]string{"conversation_id": j.ConversationID.String(), "job_id": j.ID.String()}}
	var err error
	switch {
	case j.IssueID != nil:
		err = s.jobNotifications.Create(ctx, j.UserID, j.WorkspaceID, j.IssueID, event.Type, event.Title)
	case j.TeamID != nil:
		event.SourceTeams = []string{j.TeamID.String()}
		err = s.jobNotifications.Create(ctx, j.UserID, j.WorkspaceID, nil, event.Type, event.Title, *j.TeamID)
	default:
		event.LiveOnly = true
	}
	if err != nil {
		log.WithField("job_id", j.ID).WithError(err).Warn("Could not notify about a scheduled job")
		return
	}
	publishNotification(s.jobEvents, j.WorkspaceID, j.UserID, event, s.jobNotifications)
}

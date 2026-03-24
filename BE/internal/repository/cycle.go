package repository

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/kuayle/kuayle-backend/internal/domain"
	"github.com/kuayle/kuayle-backend/internal/dto"
	"github.com/google/uuid"
	"github.com/jmoiron/sqlx"
)

type CycleRepository struct {
	db *sqlx.DB
}

func NewCycleRepository(db *sqlx.DB) *CycleRepository {
	return &CycleRepository{db: db}
}

func (r *CycleRepository) Create(ctx context.Context, cycle *domain.Cycle) error {
	query := `INSERT INTO cycles (id, team_id, name, number, status, description, start_date, end_date) VALUES ($1, $2, $3, $4, $5, $6, $7, $8) RETURNING created_at, updated_at`
	return r.db.QueryRowContext(ctx, query, cycle.ID, cycle.TeamID, cycle.Name, cycle.Number, cycle.Status, cycle.Description, cycle.StartDate, cycle.EndDate).Scan(&cycle.CreatedAt, &cycle.UpdatedAt)
}

func (r *CycleRepository) GetByID(ctx context.Context, id uuid.UUID) (*domain.Cycle, error) {
	var cycle domain.Cycle
	err := r.db.GetContext(ctx, &cycle, `SELECT * FROM cycles WHERE id = $1`, id)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return &cycle, err
}

func (r *CycleRepository) ListByTeam(ctx context.Context, teamID uuid.UUID) ([]domain.Cycle, error) {
	var cycles []domain.Cycle
	err := r.db.SelectContext(ctx, &cycles, `SELECT * FROM cycles WHERE team_id = $1 ORDER BY number DESC`, teamID)
	return cycles, err
}

func (r *CycleRepository) NextNumber(ctx context.Context, teamID uuid.UUID) (int, error) {
	var num int
	err := r.db.GetContext(ctx, &num, `SELECT COALESCE(MAX(number), 0) + 1 FROM cycles WHERE team_id = $1`, teamID)
	return num, err
}

func (r *CycleRepository) Update(ctx context.Context, cycle *domain.Cycle) error {
	query := `UPDATE cycles SET name = $1, description = $2, status = $3, start_date = $4, end_date = $5, completed_at = $6, updated_at = NOW() WHERE id = $7 RETURNING updated_at`
	return r.db.QueryRowContext(ctx, query, cycle.Name, cycle.Description, cycle.Status, cycle.StartDate, cycle.EndDate, cycle.CompletedAt, cycle.ID).Scan(&cycle.UpdatedAt)
}

func (r *CycleRepository) Delete(ctx context.Context, id uuid.UUID) error {
	_, err := r.db.ExecContext(ctx, `DELETE FROM cycles WHERE id = $1`, id)
	return err
}

// IssueStats returns total, completed, and cancelled issue counts for a cycle.
func (r *CycleRepository) IssueStats(ctx context.Context, cycleID uuid.UUID) (total int, completed int, cancelled int, err error) {
	err = r.db.QueryRowContext(ctx,
		`SELECT COUNT(*), COUNT(*) FILTER (WHERE status = 'done'), COUNT(*) FILTER (WHERE status = 'cancelled') FROM issues WHERE cycle_id = $1`,
		cycleID,
	).Scan(&total, &completed, &cancelled)
	return
}

// historyEvent represents a single issue_history row relevant to burndown.
type historyEvent struct {
	IssueID   uuid.UUID `db:"issue_id"`
	Field     string    `db:"field"`
	OldValue  *string   `db:"old_value"`
	NewValue  *string   `db:"new_value"`
	CreatedAt time.Time `db:"created_at"`
}

// issueSnapshot is the current state of an issue in the cycle at a point in time.
type issueSnapshot struct {
	Status  string
	InCycle bool
}

func isStarted(status string) bool {
	return status == "in_progress" || status == "in_review" || status == "done"
}

func isCompleted(status string) bool {
	return status == "done"
}

// BurndownData reconstructs daily scope/started/completed counts by replaying
// issue_history events for all issues that belong (or belonged) to this cycle.
func (r *CycleRepository) BurndownData(ctx context.Context, cycleID uuid.UUID, startDate, endDate time.Time) ([]dto.BurndownPoint, error) {
	// 1. Get all issues currently in the cycle with their current status.
	type issueRow struct {
		ID     uuid.UUID `db:"id"`
		Status string    `db:"status"`
	}
	var currentIssues []issueRow
	err := r.db.SelectContext(ctx, &currentIssues,
		`SELECT id, status FROM issues WHERE cycle_id = $1`, cycleID)
	if err != nil {
		return nil, err
	}

	// Collect all issue IDs we care about (current + any removed via history).
	issueIDs := make([]uuid.UUID, len(currentIssues))
	currentStatusMap := make(map[uuid.UUID]string)
	for i, iss := range currentIssues {
		issueIDs[i] = iss.ID
		currentStatusMap[iss.ID] = iss.Status
	}

	// 2. Get cycle assignment history to find issues that were added/removed.
	var cycleEvents []historyEvent
	cycleIDStr := cycleID.String()
	err = r.db.SelectContext(ctx, &cycleEvents,
		`SELECT issue_id, field, old_value, new_value, created_at
		 FROM issue_history
		 WHERE field = 'cycle' AND (old_value = $1 OR new_value = $1)
		 ORDER BY created_at ASC`, cycleIDStr)
	if err != nil {
		return nil, err
	}

	// Track all issue IDs that were ever in this cycle.
	allIssueIDSet := make(map[uuid.UUID]bool)
	for _, id := range issueIDs {
		allIssueIDSet[id] = true
	}
	for _, ev := range cycleEvents {
		allIssueIDSet[ev.IssueID] = true
	}

	if len(allIssueIDSet) == 0 {
		// No issues ever in this cycle, return empty points.
		var points []dto.BurndownPoint
		for d := startDate; !d.After(endDate) && !d.After(time.Now()); d = d.AddDate(0, 0, 1) {
			points = append(points, dto.BurndownPoint{
				Date: d.Format("2006-01-02"),
			})
		}
		return points, nil
	}

	// Build slice of all IDs for the status history query.
	allIDs := make([]uuid.UUID, 0, len(allIssueIDSet))
	for id := range allIssueIDSet {
		allIDs = append(allIDs, id)
	}

	// 3. Get all status change events for these issues.
	query, args, err := sqlx.In(
		`SELECT issue_id, field, old_value, new_value, created_at
		 FROM issue_history
		 WHERE field = 'status' AND issue_id IN (?)
		 ORDER BY created_at ASC`, allIDs)
	if err != nil {
		return nil, err
	}
	query = r.db.Rebind(query)

	var statusEvents []historyEvent
	err = r.db.SelectContext(ctx, &statusEvents, query, args...)
	if err != nil {
		return nil, err
	}

	// 4. Merge all events into a single timeline sorted by created_at.
	allEvents := make([]historyEvent, 0, len(cycleEvents)+len(statusEvents))
	allEvents = append(allEvents, cycleEvents...)
	allEvents = append(allEvents, statusEvents...)
	// Sort by time (already mostly sorted, but merge needs re-sort).
	for i := 1; i < len(allEvents); i++ {
		for j := i; j > 0 && allEvents[j].CreatedAt.Before(allEvents[j-1].CreatedAt); j-- {
			allEvents[j], allEvents[j-1] = allEvents[j-1], allEvents[j]
		}
	}

	// 5. Replay events day by day.
	// Initialize state: work backwards from current state using history.
	// Instead, start from an empty state and replay all events forward.
	snapshots := make(map[uuid.UUID]*issueSnapshot)

	// For issues currently in the cycle, determine their initial state
	// (before any history events). We assume they started as backlog in the cycle.
	// We'll correct this by replaying events.
	for id := range allIssueIDSet {
		snapshots[id] = &issueSnapshot{
			Status:  "backlog",
			InCycle: false,
		}
	}

	// Apply all events that happened before startDate to get the initial state.
	eventIdx := 0
	for eventIdx < len(allEvents) && allEvents[eventIdx].CreatedAt.Before(startDate) {
		ev := allEvents[eventIdx]
		snap := snapshots[ev.IssueID]
		if ev.Field == "cycle" {
			if ev.NewValue != nil && *ev.NewValue == cycleIDStr {
				snap.InCycle = true
			} else if ev.OldValue != nil && *ev.OldValue == cycleIDStr {
				snap.InCycle = false
			}
		} else if ev.Field == "status" && ev.NewValue != nil {
			snap.Status = *ev.NewValue
		}
		eventIdx++
	}

	// For issues currently in the cycle that had no cycle history event,
	// they may have been assigned at creation. Mark them as in-cycle.
	for _, iss := range currentIssues {
		snap := snapshots[iss.ID]
		// If still not marked in-cycle after replaying pre-start events,
		// check if there are any cycle events for this issue at all.
		hasCycleEvent := false
		for _, ev := range cycleEvents {
			if ev.IssueID == iss.ID {
				hasCycleEvent = true
				break
			}
		}
		if !hasCycleEvent {
			snap.InCycle = true
			snap.Status = iss.Status // will be corrected by status events
		}
	}

	// Re-derive statuses from status events that happened before start
	// for issues that were in cycle before start.
	// (already done above in the event replay loop)

	// 6. Walk day by day and emit points.
	today := time.Now().Truncate(24 * time.Hour)
	if endDate.After(today) {
		endDate = today
	}

	var points []dto.BurndownPoint
	for d := startDate; !d.After(endDate); d = d.AddDate(0, 0, 1) {
		nextDay := d.AddDate(0, 0, 1)

		// Apply events for this day.
		for eventIdx < len(allEvents) && allEvents[eventIdx].CreatedAt.Before(nextDay) {
			ev := allEvents[eventIdx]
			snap := snapshots[ev.IssueID]
			if ev.Field == "cycle" {
				if ev.NewValue != nil && *ev.NewValue == cycleIDStr {
					snap.InCycle = true
				} else if ev.OldValue != nil && *ev.OldValue == cycleIDStr {
					snap.InCycle = false
				}
			} else if ev.Field == "status" && ev.NewValue != nil {
				snap.Status = *ev.NewValue
			}
			eventIdx++
		}

		// Count.
		scope, started, completed := 0, 0, 0
		for _, snap := range snapshots {
			if !snap.InCycle {
				continue
			}
			scope++
			if isStarted(snap.Status) {
				started++
			}
			if isCompleted(snap.Status) {
				completed++
			}
		}

		points = append(points, dto.BurndownPoint{
			Date:      d.Format("2006-01-02"),
			Scope:     scope,
			Started:   started,
			Completed: completed,
		})
	}

	return points, nil
}

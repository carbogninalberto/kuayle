#!/usr/bin/env bash
set -e

echo "=== Carbon - Seed Data ==="

# Load env
if [ -f .env ]; then
    set -a && source .env && set +a
fi

PSQL="docker exec -i carbon-postgres-1 psql -U carbon -d carbon -q"

echo "Cleaning database..."
$PSQL <<'SQL'
-- Truncate all tables in dependency order
TRUNCATE TABLE
    favorites,
    issue_group_items,
    issue_groups,
    issue_assignees,
    team_statuses,
    issue_history,
    comments,
    issue_labels,
    issue_relations,
    issue_templates,
    notifications,
    refresh_tokens,
    views,
    webhooks,
    issues,
    cycles,
    labels,
    projects,
    project_members,
    team_members,
    workspace_members,
    teams,
    workspaces,
    users
CASCADE;
SQL
echo "Database cleaned."

echo "Generating seed data..."

# Generate bcrypt hash for "password123" using Go
cat > /tmp/hashgen.go <<'GOEOF'
package main
import (
    "fmt"
    "golang.org/x/crypto/bcrypt"
)
func main() {
    hash, _ := bcrypt.GenerateFromPassword([]byte("password123"), bcrypt.DefaultCost)
    fmt.Print(string(hash))
}
GOEOF
HASH=$(cd BE && go run /tmp/hashgen.go)
rm -f /tmp/hashgen.go

docker exec -i carbon-postgres-1 psql -U carbon -d carbon -q -v "hash=$HASH" <<'SQL'

-- ============================================================
-- USERS
-- ============================================================
INSERT INTO users (id, email, name, display_name, password_hash) VALUES
    ('a0000000-0000-0000-0000-000000000001', 'alice@carbon.dev', 'Alice Chen', 'Alice', :'hash'),
    ('a0000000-0000-0000-0000-000000000002', 'bob@carbon.dev', 'Bob Martinez', 'Bob', :'hash'),
    ('a0000000-0000-0000-0000-000000000003', 'carol@carbon.dev', 'Carol Kim', 'Carol', :'hash'),
    ('a0000000-0000-0000-0000-000000000004', 'dave@carbon.dev', 'Dave Johnson', 'Dave', :'hash'),
    ('a0000000-0000-0000-0000-000000000005', 'eve@carbon.dev', 'Eve Williams', 'Eve', :'hash');

-- ============================================================
-- WORKSPACES
-- ============================================================
INSERT INTO workspaces (id, name, slug) VALUES
    ('b0000000-0000-0000-0000-000000000001', 'Acme Corp', 'acme'),
    ('b0000000-0000-0000-0000-000000000002', 'Side Project', 'side-project');

-- Workspace Members
INSERT INTO workspace_members (workspace_id, user_id, role) VALUES
    ('b0000000-0000-0000-0000-000000000001', 'a0000000-0000-0000-0000-000000000001', 'owner'),
    ('b0000000-0000-0000-0000-000000000001', 'a0000000-0000-0000-0000-000000000002', 'admin'),
    ('b0000000-0000-0000-0000-000000000001', 'a0000000-0000-0000-0000-000000000003', 'member'),
    ('b0000000-0000-0000-0000-000000000001', 'a0000000-0000-0000-0000-000000000004', 'member'),
    ('b0000000-0000-0000-0000-000000000001', 'a0000000-0000-0000-0000-000000000005', 'guest'),
    ('b0000000-0000-0000-0000-000000000002', 'a0000000-0000-0000-0000-000000000001', 'owner'),
    ('b0000000-0000-0000-0000-000000000002', 'a0000000-0000-0000-0000-000000000003', 'member');

-- ============================================================
-- TEAMS
-- ============================================================
INSERT INTO teams (id, workspace_id, name, key, description) VALUES
    ('c0000000-0000-0000-0000-000000000001', 'b0000000-0000-0000-0000-000000000001', 'Engineering', 'ENG', 'Core product engineering'),
    ('c0000000-0000-0000-0000-000000000002', 'b0000000-0000-0000-0000-000000000001', 'Design', 'DES', 'Product design and UX'),
    ('c0000000-0000-0000-0000-000000000003', 'b0000000-0000-0000-0000-000000000001', 'Platform', 'PLT', 'Infrastructure and DevOps'),
    ('c0000000-0000-0000-0000-000000000004', 'b0000000-0000-0000-0000-000000000002', 'Core', 'CORE', 'Side project core team');

INSERT INTO team_members (team_id, user_id) VALUES
    ('c0000000-0000-0000-0000-000000000001', 'a0000000-0000-0000-0000-000000000001'),
    ('c0000000-0000-0000-0000-000000000001', 'a0000000-0000-0000-0000-000000000002'),
    ('c0000000-0000-0000-0000-000000000001', 'a0000000-0000-0000-0000-000000000004'),
    ('c0000000-0000-0000-0000-000000000002', 'a0000000-0000-0000-0000-000000000003'),
    ('c0000000-0000-0000-0000-000000000003', 'a0000000-0000-0000-0000-000000000002'),
    ('c0000000-0000-0000-0000-000000000003', 'a0000000-0000-0000-0000-000000000004'),
    ('c0000000-0000-0000-0000-000000000004', 'a0000000-0000-0000-0000-000000000001');

-- ============================================================
-- LABELS
-- ============================================================
INSERT INTO labels (id, workspace_id, name, color) VALUES
    ('d0000000-0000-0000-0000-000000000001', 'b0000000-0000-0000-0000-000000000001', 'Bug',         '#ef4444'),
    ('d0000000-0000-0000-0000-000000000002', 'b0000000-0000-0000-0000-000000000001', 'Feature',     '#3b82f6'),
    ('d0000000-0000-0000-0000-000000000003', 'b0000000-0000-0000-0000-000000000001', 'Improvement', '#8b5cf6'),
    ('d0000000-0000-0000-0000-000000000004', 'b0000000-0000-0000-0000-000000000001', 'Tech Debt',   '#f59e0b'),
    ('d0000000-0000-0000-0000-000000000005', 'b0000000-0000-0000-0000-000000000001', 'Documentation','#10b981'),
    ('d0000000-0000-0000-0000-000000000006', 'b0000000-0000-0000-0000-000000000001', 'Performance', '#ec4899'),
    ('d0000000-0000-0000-0000-000000000007', 'b0000000-0000-0000-0000-000000000001', 'Security',    '#f97316'),
    ('d0000000-0000-0000-0000-000000000008', 'b0000000-0000-0000-0000-000000000002', 'Bug',         '#ef4444'),
    ('d0000000-0000-0000-0000-000000000009', 'b0000000-0000-0000-0000-000000000002', 'Feature',     '#3b82f6');

-- ============================================================
-- PROJECTS
-- ============================================================
INSERT INTO projects (id, workspace_id, name, description, status, lead_id, start_date, target_date) VALUES
    ('e0000000-0000-0000-0000-000000000001', 'b0000000-0000-0000-0000-000000000001', 'Q1 Launch',           'Ship the v2.0 release',                     'in_progress', 'a0000000-0000-0000-0000-000000000001', '2026-01-15', '2026-03-31'),
    ('e0000000-0000-0000-0000-000000000002', 'b0000000-0000-0000-0000-000000000001', 'Auth Overhaul',       'Replace legacy auth with OAuth2 + OIDC',    'planned',     'a0000000-0000-0000-0000-000000000002', '2026-04-01', '2026-05-15'),
    ('e0000000-0000-0000-0000-000000000003', 'b0000000-0000-0000-0000-000000000001', 'Mobile App',          'Native iOS and Android clients',             'planned',     NULL,                                   '2026-06-01', '2026-09-30'),
    ('e0000000-0000-0000-0000-000000000004', 'b0000000-0000-0000-0000-000000000002', 'MVP',                 'Minimum viable product',                    'in_progress', 'a0000000-0000-0000-0000-000000000001', '2026-02-01', '2026-04-30');

-- ============================================================
-- CYCLES
-- ============================================================
INSERT INTO cycles (id, team_id, name, number, status, start_date, end_date) VALUES
    ('f0000000-0000-0000-0000-000000000001', 'c0000000-0000-0000-0000-000000000001', 'Sprint 1',  1, 'completed', '2026-02-17', '2026-03-02'),
    ('f0000000-0000-0000-0000-000000000002', 'c0000000-0000-0000-0000-000000000001', 'Sprint 2',  2, 'active',    '2026-03-03', '2026-03-16'),
    ('f0000000-0000-0000-0000-000000000003', 'c0000000-0000-0000-0000-000000000001', 'Sprint 3',  3, 'upcoming',  '2026-03-17', '2026-03-30'),
    ('f0000000-0000-0000-0000-000000000004', 'c0000000-0000-0000-0000-000000000002', 'Design Sprint 1', 1, 'active', '2026-03-03', '2026-03-16');

-- ============================================================
-- ISSUES (Engineering team)
-- ============================================================
INSERT INTO issues (id, workspace_id, team_id, project_id, cycle_id, number, identifier_text, title, description, status, priority, creator_id, assignee_id, estimate, due_date, sort_order) VALUES
    -- Sprint 2 (active) Engineering issues
    ('10000000-0000-0000-0000-000000000001', 'b0000000-0000-0000-0000-000000000001', 'c0000000-0000-0000-0000-000000000001', 'e0000000-0000-0000-0000-000000000001', 'f0000000-0000-0000-0000-000000000002', 1, 'ENG-1', 'Implement user authentication flow', '<p>Build the complete login/register flow with JWT tokens, refresh token rotation, and session management.</p>', 'in_progress', 1, 'a0000000-0000-0000-0000-000000000001', 'a0000000-0000-0000-0000-000000000002', 8, '2026-03-10', 1000),
    ('10000000-0000-0000-0000-000000000002', 'b0000000-0000-0000-0000-000000000001', 'c0000000-0000-0000-0000-000000000001', 'e0000000-0000-0000-0000-000000000001', 'f0000000-0000-0000-0000-000000000002', 2, 'ENG-2', 'Design database schema for multi-tenancy', '<p>Create the migration scripts for workspace isolation. Need advisory locks for sequential numbering.</p>', 'done', 2, 'a0000000-0000-0000-0000-000000000002', 'a0000000-0000-0000-0000-000000000002', 5, '2026-03-05', 2000),
    ('10000000-0000-0000-0000-000000000003', 'b0000000-0000-0000-0000-000000000001', 'c0000000-0000-0000-0000-000000000001', 'e0000000-0000-0000-0000-000000000001', 'f0000000-0000-0000-0000-000000000002', 3, 'ENG-3', 'Build REST API for issue CRUD', '<p>Endpoints for create, read, update, delete issues with proper validation and error handling.</p>', 'in_review', 2, 'a0000000-0000-0000-0000-000000000001', 'a0000000-0000-0000-0000-000000000004', 8, '2026-03-12', 3000),
    ('10000000-0000-0000-0000-000000000004', 'b0000000-0000-0000-0000-000000000001', 'c0000000-0000-0000-0000-000000000001', 'e0000000-0000-0000-0000-000000000001', 'f0000000-0000-0000-0000-000000000002', 4, 'ENG-4', 'WebSocket real-time updates', '<p>Implement WebSocket hub for broadcasting issue changes to connected clients.</p>', 'todo', 3, 'a0000000-0000-0000-0000-000000000002', 'a0000000-0000-0000-0000-000000000001', 5, '2026-03-14', 4000),
    ('10000000-0000-0000-0000-000000000005', 'b0000000-0000-0000-0000-000000000001', 'c0000000-0000-0000-0000-000000000001', 'e0000000-0000-0000-0000-000000000001', 'f0000000-0000-0000-0000-000000000002', 5, 'ENG-5', 'Fix N+1 query in issue list endpoint', '<p>The issue list endpoint makes separate queries for labels per issue. Batch with a single IN query.</p>', 'done', 1, 'a0000000-0000-0000-0000-000000000004', 'a0000000-0000-0000-0000-000000000004', 3, '2026-03-06', 5000),
    -- Backlog (no cycle)
    ('10000000-0000-0000-0000-000000000006', 'b0000000-0000-0000-0000-000000000001', 'c0000000-0000-0000-0000-000000000001', 'e0000000-0000-0000-0000-000000000001', NULL, 6, 'ENG-6', 'Add rate limiting to API endpoints', '<p>Implement token bucket rate limiting per user/IP to prevent abuse.</p>', 'backlog', 3, 'a0000000-0000-0000-0000-000000000001', NULL, 3, NULL, 6000),
    ('10000000-0000-0000-0000-000000000007', 'b0000000-0000-0000-0000-000000000001', 'c0000000-0000-0000-0000-000000000001', 'e0000000-0000-0000-0000-000000000002', NULL, 7, 'ENG-7', 'OAuth2 provider integration', '<p>Add support for Google, GitHub, and Microsoft OAuth2 login flows.</p>', 'backlog', 2, 'a0000000-0000-0000-0000-000000000002', 'a0000000-0000-0000-0000-000000000002', 13, NULL, 7000),
    ('10000000-0000-0000-0000-000000000008', 'b0000000-0000-0000-0000-000000000001', 'c0000000-0000-0000-0000-000000000001', NULL, NULL, 8, 'ENG-8', 'Set up CI/CD pipeline', '<p>Configure GitHub Actions for automated testing, linting, and deployment.</p>', 'todo', 2, 'a0000000-0000-0000-0000-000000000004', 'a0000000-0000-0000-0000-000000000004', 5, '2026-03-25', 8000),
    ('10000000-0000-0000-0000-000000000009', 'b0000000-0000-0000-0000-000000000001', 'c0000000-0000-0000-0000-000000000001', NULL, NULL, 9, 'ENG-9', 'Implement search with full-text indexing', '<p>Add PostgreSQL full-text search with ts_vector for fast issue search across title and description.</p>', 'backlog', 4, 'a0000000-0000-0000-0000-000000000001', NULL, 8, NULL, 9000),
    ('10000000-0000-0000-0000-000000000010', 'b0000000-0000-0000-0000-000000000001', 'c0000000-0000-0000-0000-000000000001', 'e0000000-0000-0000-0000-000000000001', NULL, 10, 'ENG-10', 'File attachment support', '<p>Allow users to upload and attach files to issues. Use S3-compatible storage.</p>', 'backlog', 4, 'a0000000-0000-0000-0000-000000000003', NULL, 8, NULL, 10000),
    ('10000000-0000-0000-0000-000000000011', 'b0000000-0000-0000-0000-000000000001', 'c0000000-0000-0000-0000-000000000001', NULL, NULL, 11, 'ENG-11', 'Email notification service', '<p>Send email notifications for issue assignments, mentions, and status changes.</p>', 'backlog', 3, 'a0000000-0000-0000-0000-000000000002', NULL, 5, NULL, 11000),
    ('10000000-0000-0000-0000-000000000012', 'b0000000-0000-0000-0000-000000000001', 'c0000000-0000-0000-0000-000000000001', 'e0000000-0000-0000-0000-000000000001', 'f0000000-0000-0000-0000-000000000003', 12, 'ENG-12', 'Implement drag-and-drop kanban board', '<p>Allow users to drag issues between status columns and reorder within columns.</p>', 'todo', 2, 'a0000000-0000-0000-0000-000000000001', 'a0000000-0000-0000-0000-000000000001', 8, '2026-03-28', 12000),
    -- Overdue issue
    ('10000000-0000-0000-0000-000000000013', 'b0000000-0000-0000-0000-000000000001', 'c0000000-0000-0000-0000-000000000001', 'e0000000-0000-0000-0000-000000000001', 'f0000000-0000-0000-0000-000000000002', 13, 'ENG-13', 'Fix broken pagination on issue list', '<p>Page 2+ returns wrong results when filters are applied. Off-by-one in offset calculation.</p>', 'in_progress', 1, 'a0000000-0000-0000-0000-000000000004', 'a0000000-0000-0000-0000-000000000002', 2, '2026-03-15', 13000),
    -- Cancelled issue
    ('10000000-0000-0000-0000-000000000014', 'b0000000-0000-0000-0000-000000000001', 'c0000000-0000-0000-0000-000000000001', NULL, 'f0000000-0000-0000-0000-000000000001', 14, 'ENG-14', 'Custom GraphQL API layer', '<p>Decided to stick with REST for now. May revisit later.</p>', 'cancelled', 4, 'a0000000-0000-0000-0000-000000000001', NULL, NULL, NULL, 14000);

-- Design team issues
INSERT INTO issues (id, workspace_id, team_id, project_id, cycle_id, number, identifier_text, title, description, status, priority, creator_id, assignee_id, estimate, due_date, sort_order) VALUES
    ('10000000-0000-0000-0000-000000000015', 'b0000000-0000-0000-0000-000000000001', 'c0000000-0000-0000-0000-000000000002', 'e0000000-0000-0000-0000-000000000001', 'f0000000-0000-0000-0000-000000000004', 1, 'DES-1', 'Design system tokens and components', '<p>Define color palette, typography scale, spacing, and base component library.</p>', 'in_progress', 2, 'a0000000-0000-0000-0000-000000000003', 'a0000000-0000-0000-0000-000000000003', 13, '2026-03-20', 1000),
    ('10000000-0000-0000-0000-000000000016', 'b0000000-0000-0000-0000-000000000001', 'c0000000-0000-0000-0000-000000000002', 'e0000000-0000-0000-0000-000000000001', 'f0000000-0000-0000-0000-000000000004', 2, 'DES-2', 'Issue detail page mockups', '<p>Create high-fidelity mockups for the full-page issue detail view with inline editing.</p>', 'done', 2, 'a0000000-0000-0000-0000-000000000003', 'a0000000-0000-0000-0000-000000000003', 5, '2026-03-08', 2000),
    ('10000000-0000-0000-0000-000000000017', 'b0000000-0000-0000-0000-000000000001', 'c0000000-0000-0000-0000-000000000002', 'e0000000-0000-0000-0000-000000000003', NULL, 3, 'DES-3', 'Mobile app wireframes', '<p>Low-fidelity wireframes for the mobile app issue list and detail views.</p>', 'backlog', 3, 'a0000000-0000-0000-0000-000000000003', NULL, 8, NULL, 3000),
    ('10000000-0000-0000-0000-000000000018', 'b0000000-0000-0000-0000-000000000001', 'c0000000-0000-0000-0000-000000000002', NULL, NULL, 4, 'DES-4', 'Onboarding flow design', '<p>Design the first-time user experience including workspace creation and team setup.</p>', 'todo', 3, 'a0000000-0000-0000-0000-000000000001', 'a0000000-0000-0000-0000-000000000003', 5, '2026-04-01', 4000);

-- Platform team issues
INSERT INTO issues (id, workspace_id, team_id, number, identifier_text, title, description, status, priority, creator_id, assignee_id, estimate, due_date, sort_order) VALUES
    ('10000000-0000-0000-0000-000000000019', 'b0000000-0000-0000-0000-000000000001', 'c0000000-0000-0000-0000-000000000003', 1, 'PLT-1', 'Kubernetes cluster setup', '<p>Provision and configure k8s cluster for staging and production environments.</p>', 'in_progress', 2, 'a0000000-0000-0000-0000-000000000002', 'a0000000-0000-0000-0000-000000000004', 13, '2026-03-25', 1000),
    ('10000000-0000-0000-0000-000000000020', 'b0000000-0000-0000-0000-000000000001', 'c0000000-0000-0000-0000-000000000003', 2, 'PLT-2', 'Set up monitoring and alerting', '<p>Deploy Prometheus + Grafana stack with alerts for API latency, error rates, and resource usage.</p>', 'todo', 3, 'a0000000-0000-0000-0000-000000000004', 'a0000000-0000-0000-0000-000000000002', 8, '2026-04-05', 2000),
    ('10000000-0000-0000-0000-000000000021', 'b0000000-0000-0000-0000-000000000001', 'c0000000-0000-0000-0000-000000000003', 3, 'PLT-3', 'Database backup automation', '<p>Automated daily PostgreSQL backups to S3 with 30-day retention and point-in-time recovery.</p>', 'backlog', 2, 'a0000000-0000-0000-0000-000000000002', NULL, 5, NULL, 3000);

-- Side project issues
INSERT INTO issues (id, workspace_id, team_id, project_id, number, identifier_text, title, status, priority, creator_id, assignee_id, sort_order) VALUES
    ('10000000-0000-0000-0000-000000000022', 'b0000000-0000-0000-0000-000000000002', 'c0000000-0000-0000-0000-000000000004', 'e0000000-0000-0000-0000-000000000004', 1, 'CORE-1', 'Landing page', 'in_progress', 2, 'a0000000-0000-0000-0000-000000000001', 'a0000000-0000-0000-0000-000000000001', 1000),
    ('10000000-0000-0000-0000-000000000023', 'b0000000-0000-0000-0000-000000000002', 'c0000000-0000-0000-0000-000000000004', 'e0000000-0000-0000-0000-000000000004', 2, 'CORE-2', 'Payment integration', 'backlog', 3, 'a0000000-0000-0000-0000-000000000001', NULL, 2000);

-- ============================================================
-- ISSUE LABELS
-- ============================================================
INSERT INTO issue_labels (issue_id, label_id) VALUES
    ('10000000-0000-0000-0000-000000000001', 'd0000000-0000-0000-0000-000000000002'),
    ('10000000-0000-0000-0000-000000000002', 'd0000000-0000-0000-0000-000000000002'),
    ('10000000-0000-0000-0000-000000000003', 'd0000000-0000-0000-0000-000000000002'),
    ('10000000-0000-0000-0000-000000000004', 'd0000000-0000-0000-0000-000000000002'),
    ('10000000-0000-0000-0000-000000000005', 'd0000000-0000-0000-0000-000000000001'),
    ('10000000-0000-0000-0000-000000000005', 'd0000000-0000-0000-0000-000000000006'),
    ('10000000-0000-0000-0000-000000000006', 'd0000000-0000-0000-0000-000000000007'),
    ('10000000-0000-0000-0000-000000000007', 'd0000000-0000-0000-0000-000000000002'),
    ('10000000-0000-0000-0000-000000000007', 'd0000000-0000-0000-0000-000000000007'),
    ('10000000-0000-0000-0000-000000000008', 'd0000000-0000-0000-0000-000000000004'),
    ('10000000-0000-0000-0000-000000000009', 'd0000000-0000-0000-0000-000000000003'),
    ('10000000-0000-0000-0000-000000000011', 'd0000000-0000-0000-0000-000000000002'),
    ('10000000-0000-0000-0000-000000000012', 'd0000000-0000-0000-0000-000000000002'),
    ('10000000-0000-0000-0000-000000000012', 'd0000000-0000-0000-0000-000000000003'),
    ('10000000-0000-0000-0000-000000000013', 'd0000000-0000-0000-0000-000000000001'),
    ('10000000-0000-0000-0000-000000000015', 'd0000000-0000-0000-0000-000000000002'),
    ('10000000-0000-0000-0000-000000000019', 'd0000000-0000-0000-0000-000000000004');

-- ============================================================
-- COMMENTS
-- ============================================================
INSERT INTO comments (issue_id, user_id, body) VALUES
    ('10000000-0000-0000-0000-000000000001', 'a0000000-0000-0000-0000-000000000002', '<p>Started working on the JWT middleware. Using RS256 for token signing.</p>'),
    ('10000000-0000-0000-0000-000000000001', 'a0000000-0000-0000-0000-000000000001', '<p>Good call on RS256. Make sure we handle token rotation properly — see the RFC 7009 spec.</p>'),
    ('10000000-0000-0000-0000-000000000003', 'a0000000-0000-0000-0000-000000000004', '<p>Ready for review. Added all CRUD endpoints with proper validation and error responses.</p>'),
    ('10000000-0000-0000-0000-000000000005', 'a0000000-0000-0000-0000-000000000004', '<p>Fixed! Used sqlx.In to batch the label query. Reduced list endpoint from ~200ms to ~15ms.</p>'),
    ('10000000-0000-0000-0000-000000000013', 'a0000000-0000-0000-0000-000000000002', '<p>Found the issue — the offset was calculated before defaults were applied. Fixing now.</p>'),
    ('10000000-0000-0000-0000-000000000015', 'a0000000-0000-0000-0000-000000000003', '<p>First draft of the design tokens is ready. Will share the Figma link shortly.</p>');

-- ============================================================
-- ISSUE HISTORY
-- ============================================================
INSERT INTO issue_history (issue_id, user_id, field, old_value, new_value) VALUES
    ('10000000-0000-0000-0000-000000000001', 'a0000000-0000-0000-0000-000000000001', 'status', 'backlog', 'todo'),
    ('10000000-0000-0000-0000-000000000001', 'a0000000-0000-0000-0000-000000000002', 'status', 'todo', 'in_progress'),
    ('10000000-0000-0000-0000-000000000002', 'a0000000-0000-0000-0000-000000000002', 'status', 'in_progress', 'done'),
    ('10000000-0000-0000-0000-000000000003', 'a0000000-0000-0000-0000-000000000004', 'status', 'in_progress', 'in_review'),
    ('10000000-0000-0000-0000-000000000005', 'a0000000-0000-0000-0000-000000000004', 'status', 'in_progress', 'done'),
    ('10000000-0000-0000-0000-000000000005', 'a0000000-0000-0000-0000-000000000004', 'priority', '2', '1'),
    ('10000000-0000-0000-0000-000000000014', 'a0000000-0000-0000-0000-000000000001', 'status', 'backlog', 'cancelled');

-- ============================================================
-- NOTIFICATIONS
-- ============================================================
INSERT INTO notifications (user_id, workspace_id, issue_id, type, title) VALUES
    ('a0000000-0000-0000-0000-000000000001', 'b0000000-0000-0000-0000-000000000001', '10000000-0000-0000-0000-000000000003', 'issue.status_changed', 'ENG-3 moved to In Review'),
    ('a0000000-0000-0000-0000-000000000001', 'b0000000-0000-0000-0000-000000000001', '10000000-0000-0000-0000-000000000005', 'issue.status_changed', 'ENG-5 marked as Done'),
    ('a0000000-0000-0000-0000-000000000002', 'b0000000-0000-0000-0000-000000000001', '10000000-0000-0000-0000-000000000001', 'issue.assigned', 'You were assigned to ENG-1'),
    ('a0000000-0000-0000-0000-000000000002', 'b0000000-0000-0000-0000-000000000001', '10000000-0000-0000-0000-000000000013', 'issue.assigned', 'You were assigned to ENG-13'),
    ('a0000000-0000-0000-0000-000000000003', 'b0000000-0000-0000-0000-000000000001', '10000000-0000-0000-0000-000000000015', 'issue.comment', 'New comment on DES-1'),
    ('a0000000-0000-0000-0000-000000000004', 'b0000000-0000-0000-0000-000000000001', '10000000-0000-0000-0000-000000000003', 'issue.comment', 'New comment on ENG-3');

-- ============================================================
-- VIEWS
-- ============================================================
INSERT INTO views (workspace_id, creator_id, name, description, filters, is_shared) VALUES
    ('b0000000-0000-0000-0000-000000000001', 'a0000000-0000-0000-0000-000000000001', 'My Urgent Issues', 'High priority issues assigned to me', '{"priority": "1,2", "assignee": "a0000000-0000-0000-0000-000000000001"}', true),
    ('b0000000-0000-0000-0000-000000000001', 'a0000000-0000-0000-0000-000000000002', 'Active Bugs', 'All open bugs', '{"status": "backlog,todo,in_progress,in_review", "label": "d0000000-0000-0000-0000-000000000001"}', true),
    ('b0000000-0000-0000-0000-000000000001', 'a0000000-0000-0000-0000-000000000001', 'Q1 Launch Tracker', 'All issues in Q1 Launch project', '{"project": "e0000000-0000-0000-0000-000000000001"}', false);

SQL

echo ""
echo "=== Seed data created ==="
echo ""
echo "  Workspaces:  Acme Corp (acme), Side Project (side-project)"
echo "  Teams:       Engineering (ENG), Design (DES), Platform (PLT), Core (CORE)"
echo "  Users:       5 users, all with password: password123"
echo ""
echo "  Login credentials:"
echo "  ┌──────────────────────┬───────────────┬──────────┐"
echo "  │ Email                │ Name          │ Role     │"
echo "  ├──────────────────────┼───────────────┼──────────┤"
echo "  │ alice@carbon.dev     │ Alice Chen    │ Owner    │"
echo "  │ bob@carbon.dev       │ Bob Martinez  │ Admin    │"
echo "  │ carol@carbon.dev     │ Carol Kim     │ Member   │"
echo "  │ dave@carbon.dev      │ Dave Johnson  │ Member   │"
echo "  │ eve@carbon.dev       │ Eve Williams  │ Guest    │"
echo "  └──────────────────────┴───────────────┴──────────┘"
echo "  Password for all: password123"
echo ""
echo "  Issues: 23 across 4 teams"
echo "  Labels: 9 (Bug, Feature, Improvement, Tech Debt, etc.)"
echo "  Projects: 4 (Q1 Launch, Auth Overhaul, Mobile App, MVP)"
echo "  Cycles: 4 (Sprint 1-3 + Design Sprint 1)"
echo ""

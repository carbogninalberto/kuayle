-- Phase 4: Cycles Enhancement
ALTER TABLE cycles ADD COLUMN status VARCHAR(20) NOT NULL DEFAULT 'upcoming' CHECK (status IN ('upcoming', 'active', 'completed'));
ALTER TABLE cycles ADD COLUMN description TEXT;
ALTER TABLE cycles ADD COLUMN completed_at TIMESTAMPTZ;

CREATE INDEX idx_cycles_team_status ON cycles(team_id, status);

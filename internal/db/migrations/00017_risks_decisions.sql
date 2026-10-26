-- +goose NO TRANSACTION
-- +goose Up
-- Risks and decisions: a project keeps a risk register and a decision log as issues of special
-- issue-type kinds, shown in their own project section and never in planning (boards, the
-- backlog, sprints, the timeline and delivery metrics). Every existing type is work.
--
-- No transaction, so widening the link-type check below never holds issue_links locked while
-- the table is scanned; each statement is safe to run again.
ALTER TABLE issue_types ADD COLUMN IF NOT EXISTS kind text NOT NULL DEFAULT 'work' CHECK (kind IN ('work', 'risk', 'decision'));

-- The project's Risks and decisions section. Turning it off hides the section and keeps the issues.
ALTER TABLE projects ADD COLUMN IF NOT EXISTS risks_decisions boolean NOT NULL DEFAULT false;

-- Links from work to the risks it mitigates and the decisions it implements, and between decisions.
-- The wider check replaces the old one without a scan (NOT VALID), then validates under a lock
-- that lets reads and writes carry on.
ALTER TABLE issue_links DROP CONSTRAINT IF EXISTS issue_links_link_type_check,
    ADD CONSTRAINT issue_links_link_type_check
    CHECK (link_type IN ('blocks', 'relates', 'duplicates', 'clones', 'mitigates', 'implements', 'supersedes')) NOT VALID;
ALTER TABLE issue_links VALIDATE CONSTRAINT issue_links_link_type_check;

-- +goose Down
UPDATE projects SET risks_decisions = false;
DELETE FROM issue_links WHERE link_type IN ('mitigates', 'implements', 'supersedes');
ALTER TABLE issue_links DROP CONSTRAINT IF EXISTS issue_links_link_type_check,
    ADD CONSTRAINT issue_links_link_type_check CHECK (link_type IN ('blocks', 'relates', 'duplicates', 'clones'));
ALTER TABLE projects DROP COLUMN IF EXISTS risks_decisions;
ALTER TABLE issue_types DROP COLUMN IF EXISTS kind;

-- Direct messages (docs/architecture/chat-and-meetings.md): a one-to-one
-- conversation between two accounts of any kind (user, agent, service
-- account). It is a workspace-scope channel of kind 'dm' with no project.
--
-- dm_key is the two account ids in sorted order, so a pair has exactly one DM
-- whichever side starts it (find-or-create is race-safe on the unique index).

ALTER TABLE channels ADD COLUMN IF NOT EXISTS dm_key TEXT;

CREATE UNIQUE INDEX IF NOT EXISTS uq_channels_dm_key ON channels (dm_key) WHERE dm_key IS NOT NULL;

ALTER TABLE channels DROP CONSTRAINT IF EXISTS chk_channels_dm;
ALTER TABLE channels ADD CONSTRAINT chk_channels_dm CHECK (
    (kind = 'dm') = (dm_key IS NOT NULL)
    AND (kind NOT IN ('dm', 'group_dm') OR scope = 'workspace')
);

-- Every account may start and read direct messages it belongs to, through the
-- platform role every account holds. The shipped USER policy now carries the
-- statements; roles that already exist get them here, unless an administrator
-- already has (a role with a DirectMessages statement is left alone). The
-- ChatCollection statement also grants messages:write on the bare collection:
-- it lets a client show the composer; every conversation is still judged by the
-- DirectMessages statement (membership).
UPDATE roles r
SET policy = jsonb_set(
        r.policy, '{statements}',
        COALESCE(r.policy->'statements', '[]'::jsonb) || jsonb_build_array(
            jsonb_build_object(
                'sid', 'ChatCollection', 'effect', 'Allow',
                'actions', '["channels:read","channels:write","messages:write"]'::jsonb,
                'resources', '["channel"]'::jsonb),
            jsonb_build_object(
                'sid', 'DirectMessages', 'effect', 'Allow',
                'actions', '["channels:read","channels:write","messages:write"]'::jsonb,
                'resources', '["channel/*"]'::jsonb,
                'conditions', '{"StringEquals":{"channel.kind":["dm"],"channel.member_ids":["${principal.id}"]}}'::jsonb))),
    updated_at = NOW()
WHERE r.name = 'USER' AND r.project_id IS NULL AND r.is_system
  AND NOT EXISTS (
      SELECT 1 FROM jsonb_array_elements(COALESCE(r.policy->'statements', '[]'::jsonb)) s
      WHERE s->>'sid' = 'DirectMessages');

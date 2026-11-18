-- Gives the project roles that already exist the chat statements the shipped
-- templates now carry (a collection statement on the project itself, which the
-- channel list and create routes are gated on, and the per-channel ones) (internal/bootstrap/defaultroles/policies). Editors read
-- and write public channels and the private ones they belong to; Analysts and
-- Viewers read them. The project Admin role holds "*" on its project and needs
-- nothing. A role that already has a statement with sid ChatPublicChannels is
-- left alone, so this is safe to re-run and never overwrites an edit.

UPDATE roles r
SET policy = jsonb_set(
        r.policy, '{statements}',
        COALESCE(r.policy->'statements', '[]'::jsonb) || jsonb_build_array(
            jsonb_build_object(
                'sid', 'ChatCollection', 'effect', 'Allow',
                'actions', CASE WHEN r.name = 'Editor'
                                THEN '["channels:read","channels:write"]'::jsonb
                                ELSE '["channels:read"]'::jsonb END,
                'resources', jsonb_build_array('project/' || r.project_id::text)),
            jsonb_build_object(
                'sid', 'ChatPublicChannels', 'effect', 'Allow',
                'actions', CASE WHEN r.name = 'Editor'
                                THEN '["channels:read","channels:write","messages:write"]'::jsonb
                                ELSE '["channels:read"]'::jsonb END,
                'resources', jsonb_build_array('project/' || r.project_id::text || '/channel/*'),
                'conditions', '{"In":{"channel.kind":["public","voice"]}}'::jsonb),
            jsonb_build_object(
                'sid', 'ChatPrivateChannels', 'effect', 'Allow',
                'actions', CASE WHEN r.name = 'Editor'
                                THEN '["channels:read","channels:write","messages:write"]'::jsonb
                                ELSE '["channels:read"]'::jsonb END,
                'resources', jsonb_build_array('project/' || r.project_id::text || '/channel/*'),
                'conditions', '{"StringEquals":{"channel.kind":["private"],"channel.member_ids":["${principal.id}"]}}'::jsonb))),
    updated_at = NOW()
WHERE r.project_id IS NOT NULL
  AND r.name IN ('Editor', 'Analyst', 'Viewer')
  AND NOT EXISTS (
      SELECT 1 FROM jsonb_array_elements(COALESCE(r.policy->'statements', '[]'::jsonb)) s
      WHERE s->>'sid' = 'ChatPublicChannels');

-- 000029_migrate_legacy_automation_actions.sql
-- The single-field action types (assign, set_status, set_priority, add_tag,
-- set_custom_field) were replaced by one update_task action that can touch
-- any number of fields at once (see ActionUpdateTask/TaskFieldUpdate in
-- domain/automation/entity.go). ValidBuiltinActionTypes and
-- AutomationConsumer.applyActionForTask no longer recognize the old type
-- strings at all, so without this migration any automation_nodes row still
-- carrying one of them would start failing every run with "unknown action
-- type" the moment this deploys. Rewrite them in place instead.
--
-- Each old action's own config fields become update_task's equivalent
-- TaskFieldUpdate field, and any existing "target" (task-retarget) config is
-- carried over unchanged since ActionConfig.Target isn't part of what
-- changed.
--
-- add_tag -> tags is the one lossy conversion here: add_tag appended a
-- single tag to whatever tags a task already had; update_task's tags field
-- is a full replacement, not a merge (see TaskFieldUpdate's doc comment).
-- There's no way to express "append" in the new shape, so a migrated add_tag
-- node will, on its next run, replace the task's tags with just the one tag
-- it used to add instead of adding it alongside the existing ones. That's a
-- real behavior change, but the alternative — leaving the node with an
-- unrecognized type — means it never runs at all, which is strictly worse.
-- Any project relying on add_tag's append semantics should review those
-- automations after this deploys.

BEGIN;

-- assign -> update_task { update: { assignee_ids: [member_id] } }
UPDATE automation_nodes
SET type = 'update_task',
    config = (CASE WHEN config ? 'target'
                   THEN jsonb_build_object('target', config->'target')
                   ELSE '{}'::jsonb
              END)
           || (CASE WHEN config ? 'member_id'
                    THEN jsonb_build_object('update',
                             jsonb_build_object('assignee_ids', jsonb_build_array(config->'member_id')))
                    ELSE '{}'::jsonb
               END)
WHERE kind = 'action' AND type = 'assign';

-- set_status -> update_task { update: { status_id: status_id } }
UPDATE automation_nodes
SET type = 'update_task',
    config = (CASE WHEN config ? 'target'
                   THEN jsonb_build_object('target', config->'target')
                   ELSE '{}'::jsonb
              END)
           || (CASE WHEN config ? 'status_id'
                    THEN jsonb_build_object('update', jsonb_build_object('status_id', config->'status_id'))
                    ELSE '{}'::jsonb
               END)
WHERE kind = 'action' AND type = 'set_status';

-- set_priority -> update_task { update: { importance: importance } }
UPDATE automation_nodes
SET type = 'update_task',
    config = (CASE WHEN config ? 'target'
                   THEN jsonb_build_object('target', config->'target')
                   ELSE '{}'::jsonb
              END)
           || (CASE WHEN config ? 'importance'
                    THEN jsonb_build_object('update', jsonb_build_object('importance', config->'importance'))
                    ELSE '{}'::jsonb
               END)
WHERE kind = 'action' AND type = 'set_priority';

-- add_tag -> update_task { update: { tags: [tag] } } — see the lossy-append
-- caveat in the header comment above.
UPDATE automation_nodes
SET type = 'update_task',
    config = (CASE WHEN config ? 'target'
                   THEN jsonb_build_object('target', config->'target')
                   ELSE '{}'::jsonb
              END)
           || (CASE WHEN config ? 'tag'
                    THEN jsonb_build_object('update',
                             jsonb_build_object('tags', jsonb_build_array(config->'tag')))
                    ELSE '{}'::jsonb
               END)
WHERE kind = 'action' AND type = 'add_tag';

-- set_custom_field -> update_task { update: { custom_fields: { field_key: value } } }
UPDATE automation_nodes
SET type = 'update_task',
    config = (CASE WHEN config ? 'target'
                   THEN jsonb_build_object('target', config->'target')
                   ELSE '{}'::jsonb
              END)
           || (CASE WHEN config ? 'field_key' AND config->>'field_key' <> ''
                    THEN jsonb_build_object('update',
                             jsonb_build_object('custom_fields',
                                 jsonb_build_object(config->>'field_key', config->'value')))
                    ELSE '{}'::jsonb
               END)
WHERE kind = 'action' AND type = 'set_custom_field';

COMMIT;

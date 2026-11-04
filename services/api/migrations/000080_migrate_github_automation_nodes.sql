-- 000080_migrate_github_automation_nodes.sql
-- The GitHub plugin's automation nodes (github.pr_linked, github.pr_state_changed,
-- github.branch_linked, github.pr_state, github.merge_pr, github.comment_pr)
-- disappeared when the plugin became a Git provider, so automations built on
-- them never fired. Core now provides the same nodes for every Git platform
-- under the git.* namespace; move the stored nodes over. The config keys
-- (expected_state, merge_method, body) are unchanged, so configs carry over as
-- is. Like every migration here this is one-way: nothing renames them back.

BEGIN;

UPDATE automation_nodes
   SET type = 'git.' || substr(type, length('github.') + 1)
 WHERE type IN (
        'github.pr_linked', 'github.pr_state_changed', 'github.branch_linked',
        'github.pr_state', 'github.merge_pr', 'github.comment_pr'
 );

COMMIT;

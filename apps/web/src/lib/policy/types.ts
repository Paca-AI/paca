/** An IAM policy document, exactly as the roles API stores and returns it. */
export interface Policy {
	version?: string;
	statements: Statement[];
}

export type Effect = "Allow" | "Deny";

export interface Statement {
	sid?: string;
	effect: Effect;
	actions: string[];
	resources: string[];
	/** operator -> condition key -> operand (a string, a bool or a list). */
	conditions?: Record<string, Record<string, unknown>>;
}

/** Where a role's checkboxes apply. A platform role is checked on the
 *  workspace-level resources (users, roles, plugins, settings, agents,
 *  projects); a project role on everything inside the project it is
 *  attached to. */
export type RoleScope = "platform" | "project";

/** One problem the server found in a policy (POST /roles/validate). */
export interface PolicyIssue {
	/** e.g. "statements[0].actions[1]". */
	path: string;
	message: string;
}

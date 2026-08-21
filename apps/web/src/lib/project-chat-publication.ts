import type { ConclusionPublication } from "./agent-api";

export interface ProjectChatPublicationSource {
	sessionId: string;
	turnId: string;
}

/**
 * Fail closed at the rendering boundary. Even a malformed or stale response
 * carrying private identifiers is unlinkable unless the server explicitly
 * marks the current viewer's source as accessible.
 */
export function projectChatPublicationSource(
	publication: ConclusionPublication,
): ProjectChatPublicationSource | null {
	if (publication.source_accessible !== true) return null;
	if (!publication.source_session_id || !publication.source_turn_id)
		return null;
	return {
		sessionId: publication.source_session_id,
		turnId: publication.source_turn_id,
	};
}

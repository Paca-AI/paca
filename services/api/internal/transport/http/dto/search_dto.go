package dto

// SearchResponse is the result of the workspace-wide search (GET /search):
// up to the requested number of matches per kind, each list limited to what
// the caller may see. A list is empty, never null, when nothing matched.
type SearchResponse struct {
	Query    string          `json:"query"`
	Projects []SearchProject `json:"projects"`
	Tasks    []SearchTask    `json:"tasks"`
	People   []SearchPerson  `json:"people"`
}

// SearchProject is one matching project.
type SearchProject struct {
	ID             string  `json:"id"`
	Name           string  `json:"name"`
	Description    string  `json:"description"`
	TaskIDPrefix   string  `json:"task_id_prefix"`
	AvatarThumbURL *string `json:"avatar_thumb_url,omitempty"`
}

// SearchTask is one matching task. TaskKey is "PRJ-12" when the project has a
// task-key prefix, otherwise "#12". The type fields describe its task type
// (Epic, Story, ...) so a client can draw it in the project's icon and color.
type SearchTask struct {
	ID             string  `json:"id"`
	ProjectID      string  `json:"project_id"`
	ProjectName    string  `json:"project_name"`
	TaskKey        string  `json:"task_key"`
	TaskNumber     int     `json:"task_number"`
	Title          string  `json:"title"`
	StatusName     *string `json:"status_name,omitempty"`
	StatusCategory *string `json:"status_category,omitempty"`
	TypeName       *string `json:"type_name,omitempty"`
	TypeIcon       *string `json:"type_icon,omitempty"`
	TypeColor      *string `json:"type_color,omitempty"`
}

// SearchPerson is one matching user. ProjectID, when set, is a project of
// theirs whose team page the caller may open.
type SearchPerson struct {
	ID             string  `json:"id"`
	Name           string  `json:"name"`
	Username       string  `json:"username"`
	AvatarThumbURL *string `json:"avatar_thumb_url,omitempty"`
	ProjectID      *string `json:"project_id,omitempty"`
}

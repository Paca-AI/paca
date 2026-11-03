package gitprovider

import "context"

// Reads and writes on repositories, change requests and issues. Each maps 1:1
// to a wire operation of the SPI (plugin-sdk-go/gitprovider/wire.go).

// GetChangeRequest reads one change request.
func (c *Client) GetChangeRequest(ctx context.Context, req CRRequest) (ChangeRequest, error) {
	return call[CRRequest, ChangeRequest](ctx, c, "get_change_request", req)
}

// ListChangedFiles pages a change request's files.
func (c *Client) ListChangedFiles(ctx context.Context, req PagedCRRequest) (ChangedFilePage, error) {
	return call[PagedCRRequest, ChangedFilePage](ctx, c, "list_changed_files", req)
}

// GetDiff returns a change request's unified diff.
func (c *Client) GetDiff(ctx context.Context, req DiffRequest) (Diff, error) {
	return call[DiffRequest, Diff](ctx, c, "get_diff", req)
}

// ListThreads pages a change request's review threads.
func (c *Client) ListThreads(ctx context.Context, req PagedCRRequest) (ThreadPage, error) {
	return call[PagedCRRequest, ThreadPage](ctx, c, "list_threads", req)
}

// ListComments pages an issue's or change request's comments.
func (c *Client) ListComments(ctx context.Context, req ListCommentsRequest) (CommentPage, error) {
	return call[ListCommentsRequest, CommentPage](ctx, c, "list_comments", req)
}

// ListIssues pages repository issues (capability issue_list).
func (c *Client) ListIssues(ctx context.Context, req ListIssuesRequest) (IssuePage, error) {
	return call[ListIssuesRequest, IssuePage](ctx, c, "list_issues", req)
}

// GetIssue reads one issue.
func (c *Client) GetIssue(ctx context.Context, req IssueRequest) (Issue, error) {
	return call[IssueRequest, Issue](ctx, c, "get_issue", req)
}

// GetChecks returns the CI results of a ref.
func (c *Client) GetChecks(ctx context.Context, req ChecksRequest) ([]Check, error) {
	return call[ChecksRequest, []Check](ctx, c, "get_checks", req)
}

// GetCheckLogs returns the tail of a check's log.
func (c *Client) GetCheckLogs(ctx context.Context, req CheckLogsRequest) (Logs, error) {
	return call[CheckLogsRequest, Logs](ctx, c, "get_check_logs", req)
}

// OpenChangeRequest opens a change request from a pushed branch.
func (c *Client) OpenChangeRequest(ctx context.Context, req OpenCRRequest) (ChangeRequest, error) {
	return call[OpenCRRequest, ChangeRequest](ctx, c, "open_change_request", req)
}

// UpdateChangeRequest edits a change request.
func (c *Client) UpdateChangeRequest(ctx context.Context, req UpdateCRRequest) (ChangeRequest, error) {
	return call[UpdateCRRequest, ChangeRequest](ctx, c, "update_change_request", req)
}

// Comment posts a top-level comment.
func (c *Client) Comment(ctx context.Context, req CommentRequest) (CommentResult, error) {
	return call[CommentRequest, CommentResult](ctx, c, "comment", req)
}

// ReplyThread replies inside a review thread.
func (c *Client) ReplyThread(ctx context.Context, req ReplyThreadRequest) (CommentResult, error) {
	return call[ReplyThreadRequest, CommentResult](ctx, c, "reply_thread", req)
}

// SubmitReview submits a review.
func (c *Client) SubmitReview(ctx context.Context, req SubmitReviewRequest) (ReviewResult, error) {
	return call[SubmitReviewRequest, ReviewResult](ctx, c, "submit_review", req)
}

// SetStatus shows run state on the platform.
func (c *Client) SetStatus(ctx context.Context, req SetStatusRequest) (StatusResult, error) {
	return call[SetStatusRequest, StatusResult](ctx, c, "set_status", req)
}

// GetFile reads a file at a ref (capability file_read).
func (c *Client) GetFile(ctx context.Context, req FileRequest) (File, error) {
	return call[FileRequest, File](ctx, c, "get_file", req)
}

// SearchCode searches code (capability code_search).
func (c *Client) SearchCode(ctx context.Context, req CodeSearchRequest) (CodeHits, error) {
	return call[CodeSearchRequest, CodeHits](ctx, c, "search_code", req)
}

// ResolveThread resolves or reopens a thread (capability resolve_threads).
func (c *Client) ResolveThread(ctx context.Context, req ResolveThreadRequest) error {
	_, err := call[ResolveThreadRequest, Empty](ctx, c, "resolve_thread", req)
	return err
}

// AddLabels adds labels (capability labels).
func (c *Client) AddLabels(ctx context.Context, req LabelsRequest) error {
	_, err := call[LabelsRequest, Empty](ctx, c, "add_labels", req)
	return err
}

// RequestReviewers requests reviewers (capability reviewer_requests).
func (c *Client) RequestReviewers(ctx context.Context, req PeopleRequest) error {
	_, err := call[PeopleRequest, Empty](ctx, c, "request_reviewers", req)
	return err
}

// SetAssignees sets assignees (capability assignees).
func (c *Client) SetAssignees(ctx context.Context, req PeopleRequest) error {
	_, err := call[PeopleRequest, Empty](ctx, c, "set_assignees", req)
	return err
}

// RerunChecks re-runs failed checks (capability rerun_checks).
func (c *Client) RerunChecks(ctx context.Context, req RerunRequest) error {
	_, err := call[RerunRequest, Empty](ctx, c, "rerun_checks", req)
	return err
}

// React adds an emoji reaction (capability reactions).
func (c *Client) React(ctx context.Context, req ReactRequest) error {
	_, err := call[ReactRequest, Empty](ctx, c, "react", req)
	return err
}

// MergeChangeRequest merges a change request (capability merge).
func (c *Client) MergeChangeRequest(ctx context.Context, req MergeRequest) (MergeResult, error) {
	return call[MergeRequest, MergeResult](ctx, c, "merge_change_request", req)
}

// GetBranch looks up a branch (capability branches).
func (c *Client) GetBranch(ctx context.Context, req GetBranchRequest) (BranchInfo, error) {
	return call[GetBranchRequest, BranchInfo](ctx, c, "get_branch", req)
}

// CreateBranch creates a branch (capability branches).
func (c *Client) CreateBranch(ctx context.Context, req CreateBranchRequest) (BranchInfo, error) {
	return call[CreateBranchRequest, BranchInfo](ctx, c, "create_branch", req)
}

// Code generated from plugin-sdk-go/gitprovider/wiretypes.go by scripts/sync-git-spi.sh. DO NOT EDIT.
// Change it in the SDK, then re-run the script. Contract: wire-shapes.golden.json.

package gitprovider

import "reflect"

// WireTypes lists every request, response and event type that crosses the
// provider wire, by name. It exists for contract tests: Paca core keeps its own
// copy of these types (it does not import this module), and both sides assert
// their JSON shapes against the same golden file (testdata/wire-shapes.golden.json)
// so a drift in either is caught by a failing test, not by a production error.
func WireTypes() map[string]reflect.Type {
	t := func(v any) reflect.Type { return reflect.TypeOf(v) }
	return map[string]reflect.Type{
		"Info": t(Info{}), "Capabilities": t(Capabilities{}), "Terms": t(Terms{}),
		"BeginSetupRequest": t(BeginSetupRequest{}), "SetupStep": t(SetupStep{}), "FormField": t(FormField{}),
		"CompleteSetupRequest": t(CompleteSetupRequest{}), "Identity": t(Identity{}), "IdentityRef": t(IdentityRef{}),
		"DeleteIdentityRequest": t(DeleteIdentityRequest{}), "DescribeIdentityRequest": t(DescribeIdentityRequest{}),
		"BeginAccessRequest": t(BeginAccessRequest{}), "CompleteAccessRequest": t(CompleteAccessRequest{}), "AccessGrant": t(AccessGrant{}),
		"RepoRef": t(RepoRef{}), "Target": t(Target{}), "SubjectRef": t(SubjectRef{}),
		"Repository": t(Repository{}), "ListReposRequest": t(ListReposRequest{}), "RepositoryPage": t(RepositoryPage{}),
		"ResolveRepoRequest": t(ResolveRepoRequest{}), "ResolvedRepository": t(ResolvedRepository{}),
		"WebhookRequest": t(WebhookRequest{}), "Event": t(Event{}), "EventSubject": t(EventSubject{}),
		"Actor": t(Actor{}), "PushInfo": t(PushInfo{}), "Branch": t(Branch{}), "Location": t(Location{}), "CheckInfo": t(CheckInfo{}), "Addressed": t(Addressed{}),
		"MintRequest": t(MintRequest{}), "Credential": t(Credential{}),
		"ActorPermissionRequest": t(ActorPermissionRequest{}), "BranchProtectionRequest": t(BranchProtectionRequest{}), "BranchProtection": t(BranchProtection{}),
		"ChangeRequest": t(ChangeRequest{}), "CRRequest": t(CRRequest{}), "PagedCRRequest": t(PagedCRRequest{}),
		"ChangedFile": t(ChangedFile{}), "ChangedFilePage": t(ChangedFilePage{}), "DiffRequest": t(DiffRequest{}), "Diff": t(Diff{}),
		"Comment": t(Comment{}), "Thread": t(Thread{}), "ThreadPage": t(ThreadPage{}),
		"ListCommentsRequest": t(ListCommentsRequest{}), "CommentPage": t(CommentPage{}),
		"IssueRequest": t(IssueRequest{}), "Issue": t(Issue{}), "ListIssuesRequest": t(ListIssuesRequest{}), "IssuePage": t(IssuePage{}),
		"Check": t(Check{}), "ChecksRequest": t(ChecksRequest{}), "CheckLogsRequest": t(CheckLogsRequest{}), "Logs": t(Logs{}),
		"FileRequest": t(FileRequest{}), "File": t(File{}), "CodeSearchRequest": t(CodeSearchRequest{}), "CodeHit": t(CodeHit{}), "CodeHits": t(CodeHits{}),
		"OpenCRRequest": t(OpenCRRequest{}), "UpdateCRRequest": t(UpdateCRRequest{}),
		"CommentRequest": t(CommentRequest{}), "CommentResult": t(CommentResult{}), "ReplyThreadRequest": t(ReplyThreadRequest{}),
		"InlineComment": t(InlineComment{}), "SubmitReviewRequest": t(SubmitReviewRequest{}), "ReviewResult": t(ReviewResult{}),
		"SetStatusRequest": t(SetStatusRequest{}), "StatusResult": t(StatusResult{}),
		"ResolveThreadRequest": t(ResolveThreadRequest{}), "LabelsRequest": t(LabelsRequest{}), "PeopleRequest": t(PeopleRequest{}),
		"MergeRequest": t(MergeRequest{}), "MergeResult": t(MergeResult{}), "RerunRequest": t(RerunRequest{}), "ReactRequest": t(ReactRequest{}),
		"GetBranchRequest": t(GetBranchRequest{}), "CreateBranchRequest": t(CreateBranchRequest{}), "BranchInfo": t(BranchInfo{}),
		"EnsureWebhookRequest": t(EnsureWebhookRequest{}), "WebhookRef": t(WebhookRef{}), "RemoveWebhookRequest": t(RemoveWebhookRequest{}),
		"Error": t(Error{}), "Envelope": t(Envelope{}),
	}
}

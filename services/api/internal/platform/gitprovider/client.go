package gitprovider

import (
	"context"
	"encoding/json"
	"errors"
)

// Doer performs one internal call to a plugin route and returns the HTTP status
// and body the plugin answered with. In production it is backed by the plugin
// runtime (plugin.Runtime.CallInternal), which does not go through the HTTP
// router: provider operations are reachable by core code only, never by a
// browser.
type Doer interface {
	Do(ctx context.Context, pluginName, method, path string, body []byte) (status int, resp []byte, err error)
}

// RoutePrefix is where a provider plugin mounts its operations (SDK: serve.RoutePrefix).
const RoutePrefix = "/_git"

// Client calls the operations of one provider plugin.
type Client struct {
	doer   Doer
	plugin string
}

// NewClient returns a client for the plugin with the given name (its id).
func NewClient(d Doer, pluginName string) *Client { return &Client{doer: d, plugin: pluginName} }

// Plugin returns the provider's plugin name.
func (c *Client) Plugin() string { return c.plugin }

// call runs one operation. Every failure is returned as an *Error so callers
// handle one error type: a provider's own error comes through as-is, and
// transport or decoding problems become CodeTransient / CodeInternal.
func call[Req, Res any](ctx context.Context, c *Client, op string, req Req) (Res, error) {
	var zero Res
	body, err := json.Marshal(req)
	if err != nil {
		return zero, &Error{Code: CodeInternal, Message: "could not encode request"}
	}
	status, raw, err := c.doer.Do(ctx, c.plugin, "POST", RoutePrefix+"/"+op, body)
	if err != nil {
		return zero, &Error{Code: CodeTransient, Message: "provider plugin is unavailable"}
	}
	var env Envelope
	if jerr := json.Unmarshal(raw, &env); jerr != nil {
		return zero, &Error{Code: CodeInternal, Message: "provider returned an unreadable response"}
	}
	if !env.OK {
		if env.Error == nil {
			return zero, &Error{Code: CodeInternal, Message: "provider failed without a reason"}
		}
		return zero, env.Error
	}
	if status >= 400 {
		return zero, &Error{Code: CodeInternal, Message: "provider reported success with an error status"}
	}
	if err := json.Unmarshal(env.Result, &zero); err != nil {
		return zero, &Error{Code: CodeInternal, Message: "provider returned a malformed result"}
	}
	return zero, nil
}

// IsCode reports whether err is a provider *Error with the given code.
func IsCode(err error, code ErrorCode) bool {
	var e *Error
	return errors.As(err, &e) && e.Code == code
}

// Describe returns the provider's static information and capabilities.
func (c *Client) Describe(ctx context.Context) (Info, error) {
	return call[Empty, Info](ctx, c, "describe", Empty{})
}

// BeginIdentitySetup starts creating an identity.
func (c *Client) BeginIdentitySetup(ctx context.Context, req BeginSetupRequest) (SetupStep, error) {
	return call[BeginSetupRequest, SetupStep](ctx, c, "begin_identity_setup", req)
}

// CompleteIdentitySetup finishes setup.
func (c *Client) CompleteIdentitySetup(ctx context.Context, req CompleteSetupRequest) (Identity, error) {
	return call[CompleteSetupRequest, Identity](ctx, c, "complete_identity_setup", req)
}

// ListRepositories pages the repositories an identity can see.
func (c *Client) ListRepositories(ctx context.Context, req ListReposRequest) (RepositoryPage, error) {
	return call[ListReposRequest, RepositoryPage](ctx, c, "list_repositories", req)
}

// ResolveRepository normalizes a URL/path and finds which candidate identity can see it.
func (c *Client) ResolveRepository(ctx context.Context, req ResolveRepoRequest) (ResolvedRepository, error) {
	return call[ResolveRepoRequest, ResolvedRepository](ctx, c, "resolve_repository", req)
}

// VerifyAndParseWebhook verifies a delivery and returns normalized events.
func (c *Client) VerifyAndParseWebhook(ctx context.Context, req WebhookRequest) ([]Event, error) {
	return call[WebhookRequest, []Event](ctx, c, "verify_and_parse_webhook", req)
}

// MintCredential returns a credential for git or API use against a repository.
func (c *Client) MintCredential(ctx context.Context, req MintRequest) (Credential, error) {
	return call[MintRequest, Credential](ctx, c, "mint_credential", req)
}

// GetActorPermission returns a login's access to a repository.
func (c *Client) GetActorPermission(ctx context.Context, req ActorPermissionRequest) (Permission, error) {
	return call[ActorPermissionRequest, Permission](ctx, c, "get_actor_permission", req)
}

// GetBranchProtection returns a branch's protection summary.
func (c *Client) GetBranchProtection(ctx context.Context, req BranchProtectionRequest) (BranchProtection, error) {
	return call[BranchProtectionRequest, BranchProtection](ctx, c, "get_branch_protection", req)
}

// EnsureWebhook registers a webhook (repo/group-scoped providers).
func (c *Client) EnsureWebhook(ctx context.Context, req EnsureWebhookRequest) (WebhookRef, error) {
	return call[EnsureWebhookRequest, WebhookRef](ctx, c, "ensure_webhook", req)
}

// RemoveWebhook removes a webhook registered by EnsureWebhook.
func (c *Client) RemoveWebhook(ctx context.Context, req RemoveWebhookRequest) error {
	_, err := call[RemoveWebhookRequest, Empty](ctx, c, "remove_webhook", req)
	return err
}

// DescribeIdentity says who an existing identity is (capability identity_description).
func (c *Client) DescribeIdentity(ctx context.Context, req DescribeIdentityRequest) (Identity, error) {
	return call[DescribeIdentityRequest, Identity](ctx, c, "describe_identity", req)
}

// DeleteIdentity asks the provider to forget an identity remotely.
func (c *Client) DeleteIdentity(ctx context.Context, req DeleteIdentityRequest) error {
	_, err := call[DeleteIdentityRequest, Empty](ctx, c, "delete_identity", req)
	return err
}

// BeginAccessGrant starts granting an identity access to one more account or
// organization (providers declaring Capabilities.AccessGrants).
func (c *Client) BeginAccessGrant(ctx context.Context, req BeginAccessRequest) (SetupStep, error) {
	return call[BeginAccessRequest, SetupStep](ctx, c, "begin_access_grant", req)
}

// CompleteAccessGrant finishes a grant from the platform's redirect.
func (c *Client) CompleteAccessGrant(ctx context.Context, req CompleteAccessRequest) (AccessGrant, error) {
	return call[CompleteAccessRequest, AccessGrant](ctx, c, "complete_access_grant", req)
}

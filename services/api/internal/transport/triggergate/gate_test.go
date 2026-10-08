package triggergate

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"

	"github.com/Paca-AI/api/internal/apierr"
	agentdom "github.com/Paca-AI/api/internal/domain/agent"
	projectdom "github.com/Paca-AI/api/internal/domain/project"
	"github.com/Paca-AI/api/internal/platform/authz/iam"
)

type recorder struct{ calls []string }

func (r *recorder) rec(name string) (*agentdom.AgentConversation, error) {
	r.calls = append(r.calls, name)
	return &agentdom.AgentConversation{TriggerType: name}, nil
}

func (r *recorder) TriggerTaskAssigned(context.Context, uuid.UUID, uuid.UUID, uuid.UUID, *uuid.UUID, string) (*agentdom.AgentConversation, error) {
	return r.rec("task")
}

func (r *recorder) TriggerDirectMessage(context.Context, uuid.UUID, uuid.UUID, *uuid.UUID, string) (*agentdom.AgentConversation, error) {
	return r.rec("direct")
}

func (r *recorder) TriggerCommentMention(context.Context, uuid.UUID, uuid.UUID, uuid.UUID, uuid.UUID, uuid.UUID, string) (*agentdom.AgentConversation, error) {
	return r.rec("comment")
}

func (r *recorder) TriggerDescriptionWrite(context.Context, uuid.UUID, uuid.UUID, uuid.UUID, uuid.UUID) (*agentdom.AgentConversation, error) {
	return r.rec("description")
}

type store struct {
	grants map[iam.Principal][]iam.Grant
	err    error
}

func (s store) ListGrants(_ context.Context, p iam.Principal) ([]iam.Grant, error) {
	return s.grants[p], s.err
}

type members map[uuid.UUID]*projectdom.ProjectMember

func (m members) FindMemberByID(_ context.Context, id uuid.UUID) (*projectdom.ProjectMember, error) {
	if mem, ok := m[id]; ok {
		return mem, nil
	}
	return nil, projectdom.ErrMemberNotFound
}

type defaultEnv struct{ env *uuid.UUID }

func (d defaultEnv) DefaultEnvironmentID(context.Context, uuid.UUID, uuid.UUID) (*uuid.UUID, error) {
	return d.env, nil
}

func grant(project uuid.UUID, sts ...iam.Statement) iam.Grant {
	return iam.Grant{RoleID: uuid.NewString(), ProjectID: project.String(), Policy: &iam.Policy{Statements: sts}}
}

func allowUse(project uuid.UUID) iam.Statement {
	return iam.Statement{Effect: iam.EffectAllow, Actions: []string{"conversations:write", "environments:read"}, Resources: []string{"project/" + project.String() + "/*"}}
}

func deny(action, resource string) iam.Statement {
	return iam.Statement{Effect: iam.EffectDeny, Actions: []string{action}, Resources: []string{resource}}
}

func TestGate(t *testing.T) {
	project, agent, env := uuid.New(), uuid.New(), uuid.New()
	userMember, agentMember := uuid.New(), uuid.New()
	user, bot := uuid.New(), uuid.New()
	u, a := iam.User(user.String()), iam.Agent(bot.String())
	p := "project/" + project.String()
	roster := members{
		userMember:  {ID: userMember, ProjectID: project, UserID: user, MemberType: "human"},
		agentMember: {ID: agentMember, ProjectID: project, MemberType: "agent", AgentID: &bot},
	}

	type tc struct {
		name      string
		grants    map[iam.Principal][]iam.Grant
		err       error
		env       *uuid.UUID
		member    uuid.UUID
		wantRun   bool
		wantForbd bool
	}
	cases := []tc{
		{name: "member with access proceeds", grants: map[iam.Principal][]iam.Grant{u: {grant(project, allowUse(project))}}, env: &env, member: userMember, wantRun: true},
		{name: "member without any grant is denied", env: &env, member: userMember, wantForbd: true},
		{name: "Deny on the agent resource", grants: map[iam.Principal][]iam.Grant{u: {grant(project, allowUse(project), deny("conversations:write", p+"/agent/"+agent.String()+"/*"))}}, env: &env, member: userMember, wantForbd: true},
		{name: "Deny on the environment resource", grants: map[iam.Principal][]iam.Grant{u: {grant(project, allowUse(project), deny("environments:read", p+"/environment/"+env.String()+"/*"))}}, env: &env, member: userMember, wantForbd: true},
		{name: "Deny on the environment is irrelevant when the agent has no environment", grants: map[iam.Principal][]iam.Grant{u: {grant(project, allowUse(project), deny("environments:read", p+"/environment/"+env.String()+"/*"))}}, env: nil, member: userMember, wantRun: true},
		{name: "agent member judged by its own grants", grants: map[iam.Principal][]iam.Grant{u: {grant(project, allowUse(project))}}, env: &env, member: agentMember, wantForbd: true},
		{name: "agent member with its own grant proceeds", grants: map[iam.Principal][]iam.Grant{a: {grant(project, allowUse(project))}}, env: &env, member: agentMember, wantRun: true},
		{name: "authorizer error denies", err: errors.New("db down"), env: &env, member: userMember},
		{name: "unknown member denies", grants: map[iam.Principal][]iam.Grant{u: {grant(project, allowUse(project))}}, env: &env, member: uuid.New()},
		{name: "no actor is not checked (even with a failing authorizer)", err: errors.New("never consulted"), env: &env, member: uuid.Nil, wantRun: true},
	}

	triggers := []struct {
		name    string
		noActor bool // the trigger can carry no member
		call    func(g *Gate, member uuid.UUID) error
	}{
		{"task assigned", true, func(g *Gate, m uuid.UUID) error {
			var by *uuid.UUID
			if m != uuid.Nil {
				by = &m
			}
			_, err := g.TriggerTaskAssigned(context.Background(), project, agent, uuid.New(), by, "")
			return err
		}},
		{"direct message", true, func(g *Gate, m uuid.UUID) error {
			var by *uuid.UUID
			if m != uuid.Nil {
				by = &m
			}
			_, err := g.TriggerDirectMessage(context.Background(), project, agent, by, "hi")
			return err
		}},
		{"comment mention", false, func(g *Gate, m uuid.UUID) error {
			_, err := g.TriggerCommentMention(context.Background(), project, agent, uuid.New(), uuid.New(), m, "hi")
			return err
		}},
		{"description write", false, func(g *Gate, m uuid.UUID) error {
			_, err := g.TriggerDescriptionWrite(context.Background(), project, agent, uuid.New(), m)
			return err
		}},
	}
	for _, tr := range triggers {
		for _, c := range cases {
			t.Run(tr.name+"/"+c.name, func(t *testing.T) {
				if c.member == uuid.Nil && !tr.noActor {
					t.Skip("this trigger always has a member")
				}
				next := &recorder{}
				g := New(next, iam.NewAuthorizer(store{grants: c.grants, err: c.err}, iam.NewRegistry(), iam.NewAttributeSchema()), roster, defaultEnv{env: c.env})
				err := tr.call(g, c.member)
				if c.wantRun {
					if err != nil || len(next.calls) != 1 {
						t.Fatalf("want the run to proceed, got err=%v calls=%v", err, next.calls)
					}
					return
				}
				if err == nil || len(next.calls) != 0 {
					t.Fatalf("want a denial with no run, got err=%v calls=%v", err, next.calls)
				}
				var apiErr *apierr.Error
				isForbidden := errors.As(err, &apiErr) && apiErr.Code == apierr.CodeForbidden
				if isForbidden != c.wantForbd {
					t.Fatalf("forbidden=%v, want %v (err: %v)", isForbidden, c.wantForbd, err)
				}
			})
		}
	}
}

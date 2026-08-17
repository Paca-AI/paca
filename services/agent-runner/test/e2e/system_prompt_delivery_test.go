package e2e_test

import (
	"context"
	"encoding/json"
	"log/slog"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/Paca-AI/agent-runner/internal/acp"
	"github.com/Paca-AI/agent-runner/internal/agent"
	"github.com/Paca-AI/agent-runner/internal/executor"
	"github.com/Paca-AI/agent-runner/test/e2e/fakellm"
)

// TestExecutorRun_SystemPromptDeliveredViaGooseHints runs Executor.Run
// against a real Docker daemon and a real goose container — not mocks —
// and inspects the exact JSON every /v1/chat/completions request sent to
// the fake LLM, the same way TestExecutorRun does. Where that test only
// checks that a call happened, this one checks *what's inside it*, because
// an earlier version of executor.go's system-prompt delivery
// (GOOSE_MOIM_MESSAGE_TEXT) looked correct by every other signal — it
// built, it passed review, Goose's own documentation described exactly the
// behavior wanted — and simply never reached the model at all. Nothing
// short of reading the real request body would have caught that, so this
// guards against regressing to that or any other channel that merely looks
// plausible.
//
// It also guards the companion claim from skills.go/hints.go: that no
// skill's full body — `paca` included, and a skill authored with no
// frontmatter at all included too, via skills.go's ensureFrontmatter — ever
// gets folded into either message directly any more, only referenced by
// name via bootstrapInstruction, with the actual content reachable solely
// through Goose's own file-based skill discovery + load_skill.
func TestExecutorRun_SystemPromptDeliveredViaGooseHints(t *testing.T) {
	if os.Getenv("PACA_E2E") != "1" {
		t.Skip("set PACA_E2E=1 to run e2e tests (requires Docker)")
	}
	checkDockerAvailable(t)
	image := agentServerImage(t)

	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	log := slog.New(slog.NewTextHandler(os.Stderr, nil))
	encryptor := newEncryptor(t)
	encryptedKey, err := encryptor.Encrypt("fake-key")
	if err != nil {
		t.Fatalf("encrypt fake key: %v", err)
	}

	gatewayIP := dockerBridgeGatewayIP(ctx, t)
	llm := fakellm.New(t, fakellm.TextReply("hello from the fake LLM"))

	sandboxMgr := newSandboxManager(t)
	exec := executor.New(sandboxMgr, encryptor, executor.Options{Image: image}, log)

	const systemPromptMarker = "MARKER-SYSTEM-PROMPT-9f3a1c"
	const pacaSkillBodyMarker = "MARKER-PACA-SKILL-BODY-7e21bd-should-never-appear-in-a-message"
	const noFrontmatterSkillName = "custom-no-frontmatter-probe"
	const noFrontmatterSkillBodyMarker = "MARKER-NO-FRONTMATTER-SKILL-BODY-4c8a02-should-never-appear-in-a-message"

	projectID := uuid.New()
	cfg := agent.Config{
		ID:              uuid.New(),
		Name:            "System Prompt Delivery Probe Agent",
		Handle:          "system-prompt-delivery-probe-agent",
		LLMProvider:     "openai",
		LLMModel:        "fake-model",
		LLMAPIKeySecret: encryptedKey,
		LLMBaseURL:      llm.BaseURL(gatewayIP),
		SystemPrompt:    systemPromptMarker,
		MaxIterations:   3,
		Skills: []agent.Skill{
			{
				SkillName: "paca",
				SkillContent: "---\nname: paca\ndescription: routes to specialized skills\n---\n\n" +
					pacaSkillBodyMarker,
				IsEnabled: true,
			},
			{
				// No frontmatter at all — this used to be exactly the case
				// that fell back to folding into buildInitialMessage's
				// prompt text. skills.go's ensureFrontmatter should
				// synthesize a minimal header instead, so this still ends
				// up file-delivered like everything else.
				SkillName:    noFrontmatterSkillName,
				SkillContent: noFrontmatterSkillBodyMarker,
				IsEnabled:    true,
			},
		},
		GitCommitterName:  "paca-agent",
		GitCommitterEmail: "agent@example.com",
	}
	trigger := agent.Trigger{
		ConversationID: uuid.New(),
		ProjectID:      projectID,
		AgentID:        cfg.ID,
		Message:        "please say hello",
		TriggerType:    agent.TriggerChatMessage,
	}

	var events []acp.Event
	result, err := exec.Run(ctx, cfg, trigger, nil, func(e acp.Event) {
		events = append(events, e)
	})
	if result.Handle != nil {
		t.Cleanup(func() {
			if err := exec.StopSandbox(context.Background(), result.Handle); err != nil {
				t.Errorf("cleanup: stop sandbox: %v", err)
			}
		})
	}
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	reqs := llm.Requests()
	if len(reqs) == 0 {
		t.Fatal("the fake LLM server was never called")
	}

	var sawSystemPromptInSystemRole bool
	var sawBootstrapInstruction bool
	for i, raw := range reqs {
		var parsed struct {
			Messages []struct {
				Role    string `json:"role"`
				Content any    `json:"content"`
			} `json:"messages"`
		}
		if err := json.Unmarshal(raw, &parsed); err != nil {
			t.Fatalf("request %d: unmarshal: %v\nraw: %s", i, err, raw)
		}
		for j, m := range parsed.Messages {
			contentBytes, _ := json.Marshal(m.Content)
			content := string(contentBytes)

			if strings.Contains(content, pacaSkillBodyMarker) {
				t.Errorf("request %d message %d (role=%s) contains the paca skill's raw body — "+
					"it must only be reachable via load_skill, never folded into a message directly:\n%s",
					i, j, m.Role, content)
			}
			if strings.Contains(content, noFrontmatterSkillBodyMarker) {
				t.Errorf("request %d message %d (role=%s) contains the no-frontmatter skill's raw body — "+
					"ensureFrontmatter should have made it file-deliverable instead of it falling back "+
					"into a message:\n%s", i, j, m.Role, content)
			}

			hasSystemPrompt := strings.Contains(content, systemPromptMarker)
			if hasSystemPrompt && m.Role != "system" {
				t.Errorf("request %d message %d: system prompt marker found in a %s-role message, "+
					"not system — it must be delivered via .goosehints into the real system prompt:\n%s",
					i, j, m.Role, content)
			}
			if hasSystemPrompt && m.Role == "system" {
				sawSystemPromptInSystemRole = true
			}
			if m.Role == "system" && strings.Contains(content, "load_skill` with `paca`") {
				sawBootstrapInstruction = true
			}
		}
	}

	if !sawSystemPromptInSystemRole {
		t.Error("the agent's system prompt never appeared in any system-role message across any request — " +
			".goosehints delivery is broken")
	}
	if !sawBootstrapInstruction {
		t.Error("no system-role message told the model to load_skill(paca) — the bootstrap instruction " +
			"that replaces paca's old always-inlined guarantee is missing")
	}

	if !discoveredAsSkill(t, events, noFrontmatterSkillName) {
		t.Errorf("skill %q (authored with no frontmatter) never showed up in an available_commands_update — "+
			"ensureFrontmatter should have made it discoverable via Goose's native skill feature instead of "+
			"it silently disappearing", noFrontmatterSkillName)
	}
}

// discoveredAsSkill reports whether name appears as a "Skill"-type entry in
// any available_commands_update event — acp.Event has no typed shape for
// that update kind (see acp.SessionUpdateKind's doc comment: only the three
// kinds this service's own event-translation path needs are named
// constants), so this decodes the raw JSON directly instead.
func discoveredAsSkill(t *testing.T, events []acp.Event, name string) bool {
	t.Helper()
	for _, e := range events {
		if string(e.Kind) != "available_commands_update" {
			continue
		}
		var parsed struct {
			AvailableCommands []struct {
				Name string `json:"name"`
				Meta struct {
					CommandType string `json:"commandType"`
				} `json:"_meta"`
			} `json:"availableCommands"`
		}
		if err := json.Unmarshal(e.Raw, &parsed); err != nil {
			t.Fatalf("unmarshal available_commands_update: %v\nraw: %s", err, e.Raw)
		}
		for _, c := range parsed.AvailableCommands {
			if c.Name == name && c.Meta.CommandType == "Skill" {
				return true
			}
		}
	}
	return false
}

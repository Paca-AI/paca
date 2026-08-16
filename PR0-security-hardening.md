# PR0 — Security / audience hardening boundary checklist

> 上游贡献（issues #392 / #397）的前置安全加固。本文档只描述 PR0 的
> 边界与验收项，是规划产物（对应云效单 PACA-1），不进入最终提交给上游的
> PR。代码锚点均基于 `master 4ca7ee2` 实读核对。

## 1. 结论摘要

当前 conversation / chat / realtime / MCP 的「谁能看/写什么」是**硬编码在
代码分支里的二分**，不是数据模型：

- global chat → owner-private（`ActorUserID`）
- 项目内一切 → project-shared（`ProjectID`，忽略「谁触发的」）

由此产生一处**跨项目读泄漏**、一处**项目内跨成员写**、以及若干「只校验
project 不校验 owner」的不一致；且数据模型无法表达「任务绑定 owner-private
chat」这种受众。PR0 的目标：**引入显式受众判别 + 统一所有读/写路径的 scope
校验 + realtime 隔离 + 收紧 MCP 鉴权**，不引入任何新功能。

## 2. 现状：受众模型的代码现实

| 类型 | 判别字段 | 当前受众 | 代码依据 |
|---|---|---|---|
| global chat conversation | `ActorUserID` | owner-private | `services/api/internal/domain/agent/entity.go:215` |
| global chat session | `ActorUserID` | owner-private | `entity.go:259` |
| project conversation（一切） | `ProjectID` + `TriggeredByMemberID` | **project-shared** | `entity.go:199-234` |
| project chat session | `MemberID` | 名义 owner，写路径不校验 | `entity.go:258` |

关键事实：`AgentConversation` 有 `TriggeredByMemberID`（谁触发的）和
`ActorUserID`（global 用），但**没有显式 audience/scope 字段**。因此「任务
绑定 owner-private chat」这类受众当前无法表达——这是 PR0 要引入的核心判别。

## 3. 不一致点清单（按严重度）

### P0-1（🔴）`ListConversationEvents` 无任何 scope 校验 → 跨项目事件泄漏

- handler `conversation_handler.go:304-322`：只 `parseParamUUID("conversationId")`，
  **完全忽略路由里的 `projectId`**。
- service `agent_service.go:1119-1121`：直接透传
  `repo.ListConversationEvents(conversationID, window)`。
- repo `agent_repository.go:1047-1109`：SQL 只有 `WHERE conversation_id = $1`。
- 路由 `router.go:740-741`：仅 `RequirePermissions(..., ProjectScopeFromParam("projectId"),
  PermissionAgentsRead)`，即「是项目 A 成员」。

**后果**：项目 A 成员只要拿到项目 B 某 conversation 的 UUID（列表/日志/共享
链接均可泄），调用 `/projects/A/conversations/<B-conv-id>/events` 即可读 B 的
全部事件。**这是唯一一个跳过 project 归属校验的读端点。**

对比：global 版 `GetGlobalConversationEvents`（handler:441-467）会先
`GetGlobalConversation` 做 owner gate——两条路径不对等。

### P0-2（🔴）`SendChatMessage` 只校验 project 不校验 member → 项目内跨成员写

- service `agent_service.go:1451-1458`：只 `session.ProjectID != projectID`，
  **不校验 `session.MemberID == memberID`**。

**后果**：同项目任何成员可向他人私有 chat session 发消息（prompt 注入）。

对比：`SendGlobalChatMessage`（agent_service.go:1586）正确校验
`*session.ActorUserID == actorUserID`。

### P0-3（🟠）`GetConversation` / `SendConversationMessage` / `ListConversations` 只有 project 维度

- `GetConversation`（agent_service.go:1106-1115）：只 `c.ProjectID != projectID`。
- `SendConversationMessage`（agent_service.go:1205-1208）：经由 `GetConversation`，同上。
- `ListConversations`（repo:855-943）：filter 仅 `project_id / actor_user_id /
  global_only / task_id / statuses / ...`，**没有「仅本人」维度**——项目列表对全
  项目返回所有 conversation（含他人私有 chat 的 metadata）。

### P0-4（🟡）`ListChatMessages` 无校验（当前死代码，接线时必须补）

- `agent_service.go:1655-1674`：注释明确 "Unreached by any route"。无 owner
  校验。将来 #397 接线历史回读时必须带 owner gate。

## 4. Realtime 隔离

- `PublishRealtime`（`services/agent-runner/internal/messaging/publisher.go:77-108`）：
  所有 conversation 事件发到单一 Pub/Sub channel `paca.events`，用 `project_id`
  或 `actor_user_id` 决定房间。
- 文档 `docs/ai-agent/realtime-events.md`：conversation 事件广播到
  **`project:<projectId>` + `conversation:<conversationId>`** 两个房间，**没有
  `user:<userId>` 房间**。

**后果**：项目内所有 conversation 的工具调用/思考/消息都进项目房间；
owner-private chat 会实时泄漏给全项目。

**需核实点**：`services/realtime`（Python，本 Go repo 之外）的 `routeEvent` 对
`actor_user_id` 是否真有 user 房间；`conversation:<id>` 房间是否有订阅鉴权。

## 5. MCP / 附件读路径

- MCP 工具按 agent 的 `permissionMap` + `config.projectId` 过滤
  （`apps/mcp/src/server.ts:68-120`）。
- 关键遗留：`!config.agentId && !config.projectId` → **"Personal API key mode,
  showing all tools"**（server.ts:89）——即 plan 中点名的「共享 API key + 声明式
  scope」问题。
- `read_task_attachment` 只要求 agent 在该 project 有 `tasks.read`
  （`apps/mcp/src/permissions.ts:291-294`），**没有 per-turn capability、没有
  「该 attachment 是否属于当前 turn/session/conversation」的校验**。

## 6. 目标受众模型（PR0 要固化的判别）

| 来源 | 转录 | 附件/结果 |
|---|---|---|
| 全局无任务 chat | owner-private | session-private |
| 项目无任务 chat | owner-private | session-private |
| 任务绑定 chat | owner-private | 新附件 task-shared；显式发布交接 task-shared |
| 无会话任务运行 | task/project-shared | 自动交接 task-shared |
| 无会话项目审计运行 | project-shared | project-shared |

## 7. 改动边界与验收项

1. **引入显式 audience 判别**：给 `AgentConversation`（+ 必要时
   `AgentChatSession`）加 `audience`/`visibility` 枚举（owner-private /
   project-shared / task-shared），migration + 约束（DB 层 enforce，不只
   service 层预检）。
2. **统一读/写路径 scope 校验**：所有 conversation/session/event/message 的
   get/list/search/send 走同一个 `canRead/canWrite` 判定，补上：
   - P0-1 的 project 归属校验（`ListConversationEvents` 必须带 projectID 或 owner gate）
   - P0-2 的 member 校验
   - P0-4 的 owner gate
3. **Realtime 隔离**：owner-private 流量走 user-scoped 房间（`user:<userId>`），
   不得同时广播进 project 房间；`conversation:<id>` 房间加订阅鉴权。
4. **MCP**：去掉「personal key 模式显示全部工具」回退；附件的 MCP 读工具带
   当前 turn/conversation/session 的 scope 校验，而非只查 agent 的 project RBAC。
5. **验收**：
   - 跨项目（A 成员读 B conversation events → 403）
   - 跨成员（A 给 B 的 session 发消息 → 403）
   - realtime（A 的私有 chat 事件不进 B 的订阅）
   - MCP（无 scope 的 key 不能读附件）

## 8. 明确不在 PR0 范围（留给后续 PR）

- turn/outbox/fencing、幂等 handoff → PR1 / #392
- 会话/Chats 历史 UI、上传生命周期、编辑器、开关/模型配置 → PR3 / #397

## 9. 对抗审查修订（2026-08-16，独立只读审查后）

审查结论：现状诊断与 P0-1~P0-4 行号全部属实；但原方案有 6 处需修正：

1. **audience 改为 STORED GENERATED 列**（`CASE WHEN chat_session_id/actor_user_id
   非空 THEN owner_private ELSE project_shared END`）。原「普通列 + DEFAULT
   'project_shared'」是 fail-open（漏设即过度公开），且与可派生真相有漂移风险。
2. **先修身份链路 bug**：`conversation_handler.go:399` 的 `SendConversationMessage`
   用 `claims.Subject`（user id）当 member id，违反 `agent_handler.go:129-133`
   的明确注释（`triggered_by_member_id`/`session.member_id` 存 `project_members.id`）。
   owner 判定前必须把该路径改为 `resolveMemberID`。
3. **owner 过滤必须下沉 SQL WHERE**：`ListConversations` 的 Search 走
   `EXISTS(agent_conversation_events …)`，若只在 service 层过滤 audience，成员可
   通过「搜索命中与否」探测他人私有 chat 的**内容**。audience/owner 过滤必须进 SQL。
4. **realtime 前提修正**：当前 `services/realtime` 是 TS/Bun 服务，**没有**
   `conversation:<id>` 房间（`docs/ai-agent/realtime-events.md` 是 Python 旧实现的
   陈旧描述）；`routeEvent` 先 project_id 短路，且 payload 对项目 chat 无 user_id
   （agent-runner 只返回 project_id/actor_user_id）。需：member→user 解析下发 +
   路由顺序改造（owner_private 优先）+ 前端项目上下文订阅 user 房间。
5. **MCP 拆两半**：去掉 personal-key 全量回退可在 PR0 做；「附件读带 turn/session
   scope」在现有 `PacaConfig`（无 conversation_id/session_id/task_id）上不可实现，
   延后到 #397 的 capability 前置 PR。
6. **`task_shared` 不是 transcript audience 的第三值**：它是附件/交接的另一个维度，
   由 #397 引入。transcript audience 就是两值（owner_private/project_shared），
   两值 generated 列不与 `task_shared` 冲突。


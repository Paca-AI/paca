# OIDC / SSO 登录（Human 单点登录）

Paca 支持接入任意标准 OIDC Provider（Keycloak、Entra ID、Okta、Authentik 等）作为**人类用户**的登录入口。

核心原则：

> **OIDC 只负责证明"这个人是谁"；Paca 继续负责 Session（Paca 自签 JWT）与授权（Global RBAC）。**

- 使用 Authorization Code Flow + PKCE (S256) + OIDC Discovery；
- 外部身份唯一键为 **(issuer, subject)** —— 绝不使用 email / username 做账号绑定；
- 登录成功后签发**与密码登录完全相同**的 Paca JWT/HttpOnly Cookie Session；
- Personal API Key、Agent MCP Key、ACP Bridge Token 等机器凭证完全不受影响；
- IdP 的 access token / ID token 不落库、不下发给浏览器或 Agent。

## 架构

```text
External IdP (Keycloak / Entra / Okta / Authentik / generic OIDC)
                │  Authorization Code + PKCE (S256)
                ▼
      Paca /api/v1/auth/oidc/login → 302 IdP
                │
      Paca /api/v1/auth/oidc/callback
                │  verify ID Token (signature/iss/aud/exp/nonce)
                ▼
      user_external_identities (issuer, sub) → Paca User
                │
      IssueSessionForUser → Paca JWT pair → HttpOnly cookies
                ▼
      既有 Paca RBAC / API Key / ACP 全部不变
```

## 配置方式与优先级

推荐在 Paca 的 **管理后台 → 工作区设置 → 单点登录** 中维护 OIDC。读取和保存该页面需要全局权限 `authentication.write`；内置 `ADMIN` 默认拥有该权限，`SUPER_ADMIN` 通过 `*` 拥有该权限。品牌设置的 `settings.write` 不授予认证配置权限。

管理后台支持配置启用状态、Issuer URL、Client ID、Client Secret、Scopes、回调地址、登录入口名称、用户名 Claim 和本地密码登录。Client Secret 是只写字段：API 只返回“是否已配置”，留空保存会保留当前有效 Secret。保存启用配置时，API 会先完成字段校验和 OIDC Discovery；只有校验、加密和数据库写入都成功后，才在处理该请求的 API 进程内原子切换。失败不会改变数据库或当前登录行为。

数据库中的 Client Secret 使用 `ENCRYPTION_KEY` 提供的 AES-256-GCM 能力加密。未配置有效 `ENCRYPTION_KEY` 时页面仍可读取，但拒绝保存 SSO 配置，不会退化成明文存储。更换或丢失该 Key 会导致已保存 Secret 无法解密，并使 API 启动失败；因此 Key 必须纳入部署密钥的备份与恢复流程。

### 环境变量引导

环境变量保留为首次部署和自动化安装的引导方式（完整列表见 `services/api/.env.example`）：

```bash
OIDC_ENABLED=true
OIDC_ISSUER_URL=https://id.example.com/realms/company
OIDC_CLIENT_ID=paca
OIDC_CLIENT_SECRET=...
OIDC_SCOPES=openid,profile,email

# 回调地址默认 = PUBLIC_URL + /api/v1/auth/oidc/callback，也可显式覆盖：
# OIDC_REDIRECT_URL=https://paca.example.com/api/v1/auth/oidc/callback

OIDC_DISPLAY_NAME=Company SSO      # 登录页 SSO 按钮文案
OIDC_USERNAME_CLAIM=preferred_username

# 置为 false 即 SSO-only：登录页隐藏密码表单，后端同时拒绝密码登录。
# 注意分阶段上线顺序（见下文「SSO-only 分阶段上线」）。
LOCAL_LOGIN_ENABLED=true
```

优先级如下：

1. 尚未在管理后台成功保存过 SSO 配置时，使用环境变量；
2. 第一次成功保存后，`workspace_settings` 成为唯一有效来源，包括显式关闭 OIDC 的状态；
3. 此后旧环境变量不会在重启时重新启用或覆盖 SSO；
4. 若保存时 Client Secret 留空，API 会保留并加密当前有效 Secret，包括首次从环境变量迁移到数据库的 Secret。

单 API 进程在保存成功后立即生效，无需重启。当前版本不在多个已运行 API 副本之间广播变更；多副本部署修改 SSO 后需要滚动重启其他副本。PostgreSQL 中的配置是重启后的统一来源。

约束（后台保存时校验，并在启动加载有效配置时 fail fast）：

- `OIDC_ENABLED=true` 时 Issuer / Client ID / Client Secret / 回调地址（显式或由 `PUBLIC_URL` 推导）缺一不可；
- issuer 与回调地址必须 HTTPS（`http://localhost` 等环回地址例外，便于本地联调）；
- issuer 是**标识符**而非可规范化 URL：配置原样使用（只去首尾空白，保留尾斜杠），必须与 Discovery metadata 及 ID Token `iss` 完全一致；
- `ENV=production` 时回调地址必须 HTTPS；
- IdP Discovery 在启用配置保存前和 API 启动时执行，IdP 不可达时保存失败或 API 拒绝启动；对 IdP 的所有出站调用（Discovery、code exchange、JWKS、UserInfo）走独立 HTTP client，10 秒超时——半死不活的 IdP 会快速失败而不是挂住启动或请求；
- JIT 用户的 Global Role 固定为内置 `USER`（不可配置——Global Role 是可自定义权限集，任何可配置的默认角色都可能被指向高权限自定义角色；特权角色只能在 Paca 内手动授予）；
- `LOCAL_LOGIN_ENABLED=false` 时保存和启动均校验：必须已存在至少一个绑定**当前 issuer** SSO 的 `ADMIN`/`SUPER_ADMIN` 用户，否则拒绝变更或启动（防止管理员锁死；换 IdP 后旧绑定不满足守卫，见下文）。

### SSO-only 分阶段上线（必须按序）

JIT 用户永远不会自动获得 `ADMIN`/`SUPER_ADMIN`，而 Paca 的初始管理员是本地密码账号。若一开始就 `LOCAL_LOGIN_ENABLED=false`，将没有人能管理系统。因此分阶段上线由保存与启动守卫共同强制：

1. `OIDC_ENABLED=true` + `LOCAL_LOGIN_ENABLED=true`（先两者并存）；
2. 目标管理员通过 SSO 登录，JIT 建号（Global Role 固定为 USER）；
3. 本地 admin 在 Paca 内将该 SSO 用户提升为 `ADMIN`；
4. 验证该 SSO 管理员可正常登录、管理；
5. 最后才在后台关闭“本地密码登录”（或设置 `LOCAL_LOGIN_ENABLED=false`）——守卫检测到**当前 issuer** 下存在特权 SSO 用户才放行；若日后更换 IdP，需在新 issuer 下重新完成上述晋升步骤，旧 IdP 的绑定不会让新配置通过守卫。

在 IdP 侧注册 confidential web client，回调地址填：

```text
https://<你的 Paca 域名>/api/v1/auth/oidc/callback
```

## 用户与身份模型

```sql
user_external_identities (
    user_id  → users.id,
    issuer   TEXT,   -- IdP issuer URL
    subject  TEXT,   -- OIDC sub
    UNIQUE (issuer, subject)
)
```

- **JIT 建号**（始终开启）：首个登录的 (iss, sub) 自动创建 Paca 用户。用户名取自配置的 claim（默认 `preferred_username`，净化后不足 3 位或超长则回退 `sso-<sha256(issuer+subject) 前 12 位 hex>`，绝不使用原始 sub），冲突时追加 `-2`、`-3`；email 冲突时**直接放弃该 email**（绝不据此绑定既有账号）。同一 (iss, sub) 并发首登录撞唯一索引时，输家回读赢家已建立的绑定并复用其用户，而不是登录失败。
- **Profile 来源**：身份只认 ID Token 的 (iss, sub)；`preferred_username`/`name`/`email` 优先从 ID Token 读取，缺失时才由 UserInfo endpoint 补齐（很多标准 IdP 只在 UserInfo 提供 profile/email claims）。UserInfo 返回的 `sub` 必须与 ID Token 一致，否则整个忽略；配置的 `OIDC_USERNAME_CLAIM` 对两个来源同样生效。
- **email 门槛**：只有 IdP 明确给出 `email_verified=true` 的 email 才会写入用户记录（email 用于通知等场景，未验证的地址不落库）。
- JIT 用户是 **SSO-only 账号**：写入未知随机密码 + `password_login_enabled=false`，且 `must_change_password=false`。密码登录、改密、管理员重置、密码设置链接对其全部 fail closed（`USER_PASSWORD_LOGIN_DISABLED`），防止意外打开第二条登录路径。
- **不按 email 自动绑定**已有账号 —— email 可变、可被回收，跨 issuer 不等价；把"属性匹配"当"所有权证明"是账号接管漏洞。确有需要时由管理员手动绑定（后续版本提供 Admin Link）。

## 登录页行为

登录页启动时请求公开端点：

```text
GET /api/v1/auth/config
→ { "local_login_enabled": true,
    "oidc": { "enabled": true, "display_name": "Company SSO" } }
```

- OIDC 启用 → 显示 "Continue with {display name}" 按钮，仅做浏览器跳转 `/api/v1/auth/oidc/login`（SPA 不接触 client_secret / code / token）；
- `LOCAL_LOGIN_ENABLED=false` → 隐藏整个密码表单；后端在 service 层同步拒绝密码登录（`AUTH_LOCAL_LOGIN_DISABLED`，403）；
- 回调成功或失败后，后端使用同源相对地址回到首页；浏览器会保留实际接收 callback 的域名、协议和端口，不依赖启动时的 `PUBLIC_URL`。失败时附带 `?sso_error=1`，前端展示**通用**错误（不回显 IdP 错误详情）。

## Logout

`POST /api/v1/auth/logout` 仍只吊销 Paca token family 并清 Cookie，**不**强制注销 IdP（避免影响用户在其他企业应用的会话）。可选的"同时登出 IdP"为后续演进。

## Keycloak 参考配置

以 Keycloak 26.x 为例：

1. 建立 Realm（如 `paca`），记下 issuer：`https://<keycloak>/realms/paca`（注意**无** `/protocol/openid-connect` 后缀）。
2. Clients → Create client：
   - Client type: **OpenID Connect**；
   - Client ID: `paca`；
   - Client authentication: **On**（confidential client，Paca 服务端做 code exchange）；
   - Authentication flow: 勾选 **Standard flow**（Authorization Code）；
   - Valid redirect URIs: `https://paca.example.com/api/v1/auth/oidc/callback`（精确匹配）；
   - Valid post logout redirect URIs / Web origins: 按需（MVP 未用到）。
3. Clients → paca → Credentials：复制 Client secret，填入 Paca 管理后台的 SSO 配置（或首次引导所用的 `OIDC_CLIENT_SECRET`）。
4. Realm Settings → Keys：确认有 RS256 活动密钥（Paca 通过 Discovery 的 jwks_uri 自动取公钥并支持轮换）。
5. 在管理后台填写 Issuer URL、Client ID、Client Secret，并保持本地密码登录开启后保存。也可以先用环境变量引导：

```bash
OIDC_ENABLED=true
OIDC_ISSUER_URL=https://keycloak.example.com/realms/paca
OIDC_CLIENT_ID=paca
OIDC_CLIENT_SECRET=<client-secret>
PUBLIC_URL=https://paca.example.com
```

6. 管理后台保存成功后，当前 API 进程的登录页立即出现 SSO 按钮。环境变量引导需要重启 API；启动日志出现 `oidc runtime initialized` 且 `enabled=true` 即接入成功。

## 管理 API

```text
GET   /api/v1/admin/settings/sso
PATCH /api/v1/admin/settings/sso
```

两个接口都要求用户 JWT 会话和 `authentication.write`；个人 API Key 与 Agent API Key 均不允许读写全站登录配置。响应不包含明文 Secret 或数据库密文，只包含 `client_secret_configured` 和 `encrypted_secret_storage_available`。公开的 `GET /api/v1/auth/config` 每次读取当前运行时快照，因此登录页会跟随同进程内的成功保存立即更新。

本地联调可用 `http://localhost:8080/realms/...` 形式的 issuer（环回地址允许 http），Keycloak 可用 `deploy/docker-compose.dev.yml` 同网络起容器。

## 安全要点（实现内建）

- Authorization Code + PKCE S256、state、nonce、精确回调 URI 匹配；
- **Login CSRF 防御**：`/oidc/login` 同时向浏览器写入短期（10 分钟）HttpOnly `SameSite=Lax` 的 state cookie（Path 限定回调端点），回调必须「URL state ↔ cookie」匹配才被接受——把回调 URL 转发给受害者浏览器、借其完成攻击者会话的 swapping 攻击因此失效；cookie 在回调的所有出口（成功/失败/IdP 报错）一律清除，state 本身单次消费；
- ID Token 校验：签名/JWKS、issuer、audience、expiry、nonce（全部由成熟库 go-oidc 完成，JWKS 缓存并支持轮换）；
- 登录事务（state→nonce/verifier）存于 Valkey，TTL 10 分钟、**GETDEL 单次消费**——重放回调 URL 无效；
- 对 IdP 的所有出站调用（Discovery/exchange/JWKS/UserInfo）统一 10 秒超时；
- 审计日志只记录 issuer、Paca user id、成败类别，**不记录** token / code / 内部错误详情；
- 回调响应带 `Referrer-Policy: no-referrer`；
- 错误一律泛化，不泄露 Provider 内部响应。
- SSO-only 模式由 PostgreSQL 延迟约束持续保护：降权、删除用户、角色改名或身份绑定变更若会移除最后一名同 issuer 的 SSO 管理员，事务会以 `AUTH_SSO_ADMIN_REQUIRED` 拒绝；并发写入也通过单例设置行串行判定。

## 明确不做（后续演进）

SAML、LDAP、SCIM、IdP Group→Role 同步、多 IdP、Workspace 级 IdP、Social Login、MCP Remote OAuth、Back-channel Logout、IdP 会话实时同步、Admin Link / 受控 email 绑定。

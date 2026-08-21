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

## 配置

在 Paca API 的环境变量中启用（完整列表见 `services/api/.env.example`）：

```bash
OIDC_ENABLED=true
OIDC_ISSUER_URL=https://id.example.com/realms/company
OIDC_CLIENT_ID=paca
OIDC_CLIENT_SECRET=...
OIDC_SCOPES=openid,profile,email

# 回调地址默认 = PUBLIC_URL + /api/v1/auth/oidc/callback，也可显式覆盖：
# OIDC_REDIRECT_URL=https://paca.example.com/api/v1/auth/oidc/callback

OIDC_DISPLAY_NAME=Company SSO      # 登录页 SSO 按钮文案
OIDC_JIT_PROVISION=true            # 首次登录自动建号
OIDC_DEFAULT_ROLE=USER             # JIT 用户的 Global Role（禁止 ADMIN/SUPER_ADMIN）
OIDC_USERNAME_CLAIM=preferred_username

# 置为 false 即 SSO-only：登录页隐藏密码表单，后端同时拒绝密码登录
LOCAL_LOGIN_ENABLED=true
```

约束（启动时 fail fast）：

- `OIDC_ENABLED=true` 时 Issuer / Client ID / Client Secret / 回调地址（显式或由 `PUBLIC_URL` 推导）缺一不可；
- issuer 与回调地址必须 HTTPS（`http://localhost` 等环回地址例外，便于本地联调）；
- `ENV=production` 时回调地址必须 HTTPS；
- IdP Discovery 在**启动时**执行，IdP 不可达则 API 拒绝启动；
- `OIDC_DEFAULT_ROLE` 必须是已存在的 Global Role，且不能是 `ADMIN`/`SUPER_ADMIN`（特权角色只能在 Paca 内手动授予）。

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

- **JIT 建号**（默认开启）：首个登录的 (iss, sub) 自动创建 Paca 用户。用户名取自配置的 claim（默认 `preferred_username`，净化后不足 3 位则回退 `sso-<sub前8位>`），冲突时追加 `-2`、`-3`；email 冲突时**直接放弃该 email**（绝不据此绑定既有账号）。
- JIT 用户是 **SSO-only 账号**：写入未知随机密码 + `password_login_enabled=false`，且 `must_change_password=false`。密码登录、改密、管理员重置、密码设置链接对其全部 fail closed（`USER_PASSWORD_LOGIN_DISABLED`），防止意外打开第二条登录路径。
- **不按 email 自动绑定**已有账号 —— email 可变、可被回收，跨 issuer 不等价；把"属性匹配"当"所有权证明"是账号接管漏洞。确有需要时由管理员手动绑定（后续版本提供 Admin Link）。
- `OIDC_JIT_PROVISION=false` 时，未知 (iss, sub) 登录被拒绝（`AUTH_SSO_NOT_PROVISIONED` 语义，前端只显示通用错误）。

## 登录页行为

登录页启动时请求公开端点：

```text
GET /api/v1/auth/config
→ { "local_login_enabled": true,
    "oidc": { "enabled": true, "display_name": "Company SSO" } }
```

- OIDC 启用 → 显示 "Continue with {display name}" 按钮，仅做浏览器跳转 `/api/v1/auth/oidc/login`（SPA 不接触 client_secret / code / token）；
- `LOCAL_LOGIN_ENABLED=false` → 隐藏整个密码表单；后端在 service 层同步拒绝密码登录（`AUTH_LOCAL_LOGIN_DISABLED`，403）；
- 回调失败 → 后端 302 回首页并带 `?sso_error=1`，前端展示**通用**错误（不回显 IdP 错误详情）。

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
3. Clients → paca → Credentials：复制 Client secret 填入 `OIDC_CLIENT_SECRET`。
4. Realm Settings → Keys：确认有 RS256 活动密钥（Paca 通过 Discovery 的 jwks_uri 自动取公钥并支持轮换）。
5. Paca 侧环境变量：

```bash
OIDC_ENABLED=true
OIDC_ISSUER_URL=https://keycloak.example.com/realms/paca
OIDC_CLIENT_ID=paca
OIDC_CLIENT_SECRET=<client-secret>
PUBLIC_URL=https://paca.example.com
```

6. 重启 Paca API。启动日志出现 `oidc sso enabled` 即接入成功；登录页应出现 SSO 按钮。

本地联调可用 `http://localhost:8080/realms/...` 形式的 issuer（环回地址允许 http），Keycloak 可用 `deploy/docker-compose.dev.yml` 同网络起容器。

## 安全要点（实现内建）

- Authorization Code + PKCE S256、state、nonce、精确回调 URI 匹配；
- ID Token 校验：签名/JWKS、issuer、audience、expiry、nonce（全部由成熟库 go-oidc 完成，JWKS 缓存并支持轮换）；
- 登录事务（state→nonce/verifier）存于 Valkey，TTL 10 分钟、**GETDEL 单次消费**——重放回调 URL 无效；
- 审计日志只记录 issuer、Paca user id、成败类别，**不记录** token / code / 内部错误详情；
- 回调响应带 `Referrer-Policy: no-referrer`；
- 错误一律泛化，不泄露 Provider 内部响应。

## 明确不做（后续演进）

SAML、LDAP、SCIM、IdP Group→Role 同步、多 IdP、Workspace 级 IdP、Social Login、MCP Remote OAuth、Back-channel Logout、IdP 会话实时同步、Admin Link / 受控 email 绑定。

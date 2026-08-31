# cpa-provider-nexus 开发知识库

维护约定：本文档是长期知识库，不是交接快照。每次排查出新根因、变更部署方式
或发现新的坑，把结论沉淀到对应章节；过期内容直接改写，不要另开新文件。

## 项目定位

`cpa-provider-nexus` 是 CLIProxyAPI（CPA）的独立 Linux 原生插件，不修改 CPA 主包。
插件以一个稳定身份聚合多个认证和模型来源；当前支持 Kiro 与 Cline。运行时只
需要 CPA 和插件 `.so`；`kiro-gateway`、`Kiro-Go`、`quotio-desktop` 仅作为
协议和登录流程参考。

仓库：<https://github.com/ViceEye/cpa-provider-nexus>

架构总览与目录职责见 `AGENTS.md`；本文档记录运行时行为、流程细节和经验教训。

### 身份约定

| 层级 | 值 |
| --- | --- |
| 项目、插件 ID、配置键 | `cpa-provider-nexus` |
| CPA Provider ID、凭证 `type` | `nexus` |
| 模型前缀 | `nexus/` |
| Kiro 凭证来源 | `kind: "kiro"` |
| Cline 凭证来源 | `kind: "cline"` |

`type` 用于 CPA 把认证记录路由到本插件，`kind` 只用于插件内部选择实际协议。
插件只接受 `type: "nexus"`，不保留旧 Provider ID 的兼容分支。

## 运行时与插件 ABI

- 插件是 Linux amd64 glibc 的 `c-shared` `.so`，由 CPA 宿主加载。
- C ABI：宿主通过 `cliproxy_plugin_init` 传入函数表，插件用
  `cliproxyPluginCall` 以 JSON 信封 `{ok, result, error}` 双向 RPC。
- 所有上游 HTTP 必须走宿主桥（`host.http.do` / `host.http.do_stream`），
  插件内不得直连网络。
- 控制台单文件 `internal/provider/console/index.html` 通过 go:embed 打进
  `.so`，改前端必须重建 `.so` 才生效。

## 凭证与认证体系

### 登录模式

| `login_mode` | 流程 | 适用 |
| --- | --- | --- |
| `kiro-browser`（默认） | `app.kiro.dev/signin` PKCE，回环回调 `http://localhost:3128` | 本机或可中继回调的场景 |
| `aws-device` | AWS 设备码授权，无浏览器回调 | 远程 CPA 推荐 |

- 组织账号（`sso_start_url` 为非默认 `*.awsapps.com/start`）在浏览器流程中
  会返回组织验证链接并转入设备码继续，最终凭证标记为 **Kiro Organization**。
- Kiro 生产登录页拒绝任意公网 redirect URI（只允许 localhost 或
  `app.kiro.dev` 子域），不要把 CPA 域名配成 `browser_redirect_uri`。

### 凭证 ID 策略（关键不变量）

- CPA 的认证记录 ID 由**物理认证文件的相对路径**推导（带 `.json` 后缀），
  文件扫描（`auth.parse`）和 `host.auth.save` 用同一套规则。
- 插件在 `auth.parse`（单账号文件）、命令行导入、`auth.refresh`、模型发现
  更新中**不得**返回自己的内容哈希 ID；返回空 ID/FileName 让宿主兜底，保存
  才能原地更新。**例外**：多账号文件必须保留逐账号内容哈希 ID，路径 ID 会
  在账号间碰撞。
- 凭据 JSON 内部的 `auth_id`（`kiro-<sha256前10字节>`）只用于插件内统计
  匹配，与 CPA 记录 ID 无关，刷新/重登后内容哈希变化是正常现象。
- 宿主记录 ID 不得写进凭据 JSON；刷新响应要合并请求 Attributes 以保留
  文件路径属性；重序列化凭据时必须从原存储 JSON 合并未建模字段
  （`disabled`/`priority`/`note`），否则刷新会丢停用标记。

### 凭证导入

`--kiro-import` 支持：Kiro IDE JSON、AWS SSO 缓存 JSON、Enterprise
`clientIdHash` 注册、kiro-cli / Amazon Q SQLite（`auth_kv`）、目录批量。
`reference` 模式跟随源文件刷新，`copy` 模式存独立副本。

### Cline 来源

- Cline 凭证以 `type: "nexus", kind: "cline"` 存储，刷新后仍保持该结构。
- OAuth 使用浏览器回调 URL 换取 token；所有请求继续通过 CPA 的宿主 HTTP 桥，
  插件本身不直连网络。
- Cline 模型目录和对话响应使用其 API 信封，插件解包后统一注册为
  `nexus/<vendor>/<model>` 并输出标准 OpenAI Chat Completions 格式。
- Cline 免费模型没有 Kiro 订阅额度结构；面板只展示其上游实际可取得的余额或
  错误状态。

## OAuth / 重新登录流程

顶部 OAuth 登录（`/console/oauth/*`）和认证文件卡片“重新登录”
（`/oauth/relogin/*`）共用底层 `startLogin` / `pollLogin`，但**保存语义不同**：

- 顶部 OAuth：新增凭证，保存为 `authData.FileName`（新内容哈希名）。
- 卡片重新登录：保存回**原认证文件名**，并保留旧凭证中未被新响应覆盖的字段
  （`relogin.go` 的 merge 逻辑）。

浏览器流程步骤：

1. 插件返回 Kiro 登录 URL 和 `state`。
2. 浏览器完成登录后复制 localhost 回调 URL，粘贴提交到 `/oauth/callback`。
3. 组织账号返回 AWS device 验证链接（`processBrowserCallback` 转
   `beginDeviceAuthorization`），前端展示链接与设备码并继续轮询。
4. 成功后前端清空流程 UI（v0.7.6 起），仅保留成功提示。

## 错误与状态映射

- `401`/`403`：刷新一次并重试，仍失败则上抛状态码。
- `402`/`429`：直接上抛，由 CPA 做冷却和账号切换。
- 网络错误与 `5xx`：可重试上游故障；畸形客户端载荷：不可重试 `400`。
- 令牌：到期前 10 分钟 CPA 计划刷新；请求前发现临期/缺失也会先刷新；
  轮换后的 token 写回原认证条目。8 小时正常过期无需人工干预。

## 请求限制

- Kiro 上游限制约 600 KiB / 900 KiB。转换器的压缩、图片丢弃、工具结果截断、
  历史裁剪是**降级策略**，不是无限上下文支持。
- Kiro Claude 模型对工具 schema 和历史 tool call 校验严格：转换器会清理孤立
  调用、展开顶层 `oneOf/allOf/anyOf`、合并重复 `toolUseEvent` 分片。

## 构建与发布

```powershell
# 1. 控制台（改了 console-ui/src 才需要）
cd internal/provider/console-ui && npm run build
Copy-Item dist\index.html ..\console\index.html -Force

# 2. 插件 .so（Docker Desktop 需在运行；自动跑 gofmt/vet/test）
$cfg = Join-Path $env:TEMP ("nexus_dockercfg_" + [guid]::NewGuid().ToString("N"))
New-Item -ItemType Directory -Path $cfg | Out-Null
$env:DOCKER_CONFIG = $cfg
docker build --output type=local,dest=dist .
```

发版同步三处版本号：`internal/provider/types.go` 的 `pluginVersion`、
`Dockerfile` 产物名、README 版本说明。

## 服务器部署

生产服务器（下例以 `cpa.example.com` 指代，Docker 容器 `cli-proxy-api`）：

| 宿主路径 | 容器路径 | 内容 |
| --- | --- | --- |
| `/root/CLIProxyAPI/config.yaml` | `/CLIProxyAPI/config.yaml` | CPA 配置（插件配置在其 `plugins.configs.cpa-provider-nexus`） |
| `/root/CLIProxyAPI/auths` | `/root/.cli-proxy-api` | 认证文件目录（`auth-dir`） |
| `/root/CLIProxyAPI/plugins` | `/CLIProxyAPI/plugins` | 插件目录（旧版本改名加后缀即可，非 `.so` 结尾不会被加载） |

部署检查清单：

1. 把配置键从旧的 `plugins.configs.kiro-provider` 改为
   `plugins.configs.cpa-provider-nexus`，并移除旧插件 `.so`。
2. 上传 `.so` 和 `.sha256` 到服务器 `/tmp/`，`sha256sum -c` 校验。
3. 备份插件目录中的旧 `.so`（改名，如 `.bak-<说明>`）。
4. 安装新 `.so` 并再次校验。
5. `docker restart cli-proxy-api`。
6. 日志确认 `plugin registered plugin_id=cpa-provider-nexus version=...`。
7. `/auth-files` 确认 Kiro 记录数与磁盘文件数一致（防重复注册回归），
   `/v1/models` 确认模型出现，再做一次 Chat Completions 测试。

服务器部署与 Git 推送分开执行；未明确要求时不要自动更新服务器。

## 历史决策与根因记录

### v0.7.6 / v0.7.7（2026-08-29）重新登录后重复凭证

- **现象**：重新登录成功后原凭证刷新了，但面板多出一个新凭证；且旧版本会话
  中曾出现磁盘上多出 `kiro-<hash>.json` 文件。
- **根因**：插件 `auth.parse` 返回凭据内容哈希 ID（`kiro-<hash>`，无
  `.json`），CPA `host.auth.save` 按文件路径注册 ID（`xxx.json`）。同一物理
  文件被挂两条管理器记录（`GetByID` 找不到路径 ID 记录就 `Register`）。
- **修复**：插件在 `auth.parse`（仅单账号文件）、导入、刷新、模型发现更新中
  不再自带 ID；宿主记录 ID 不写入凭据 JSON；`persistCredentialBestEffort`
  不再盲目拼 `.json` 后缀。**多账号文件必须保留逐账号内容哈希 ID**——
  路径 ID 会在账号间碰撞（v0.7.7 review 发现并回归测试锁定）。
- **附带修复**：`auth.refresh` 响应原来直接重序列化凭据结构体，会丢掉插件
  未建模的存储字段（`disabled`/`priority`/`note`），刷新一次就把停用的凭证
  复活；现在从原存储 JSON 合并未知字段回来。
- **验证方式**：重启后 `/auth-files` 仅一条 Kiro 记录，触发真实 token 刷新
  后仍是一条；`TestRefreshAuthPreservesUnknownStoredFields` 锁定字段合并。

### 其他已知事项

- Kiro session/refresh token 可能过期且不可刷新，需要通过卡片“重新登录”
  重新授权。
- `profileArn` 缺失时插件自动发现；上游仍拒绝时需重新登录或导入完整凭证。
- 当前工作树包含未提交的历史开发改动，**禁止 `git reset --hard` 清理**。
- 临时调试脚本放 `temp/`（gitignore），正式测试在 `integration/`。

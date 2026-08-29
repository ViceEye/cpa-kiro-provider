# kiro-provider Agent 指南

CLIProxyAPI（CPA）的独立 Linux 原生插件（AGPL-3.0），把 Kiro 运行时模型通过
标准 OpenAI Chat Completions API 暴露出去。仓库：
<https://github.com/ViceEye/cpa-kiro-provider>

## 目录结构

| 路径 | 用途 |
| --- | --- |
| `cmd/kiro-provider/` | 插件入口。C ABI（`cliproxy_plugin_init` 等），通过 JSON 信封 `{ok, result, error}` 与 CPA 宿主双向 RPC |
| `internal/provider/` | 核心：方法分发（`handler.go`）、OAuth/登录（`oauth.go`、`oauth_management.go`、`console_oauth.go`、`relogin.go`）、凭据（`credentials.go`、`auth.go`）、执行（`executor.go`）、模型（`models.go`）、配额（`quota.go`）、管理路由（`service.go`、`stats.go`） |
| `internal/provider/console/` | 内嵌控制台成品（单文件 `index.html`，go:embed 进 `.so`） |
| `internal/provider/console-ui/` | 控制台 React/Vite 源码（`src/main.jsx`） |
| `internal/chat/` | OpenAI ↔ Kiro 请求/响应转换（历史归一化、工具 schema 修复、900 KiB 降级） |
| `internal/eventstream/` | AWS 二进制 Event Stream 解析（CRC 校验、toolUseEvent 合并） |
| `internal/jsonx/` | JSON 取值辅助 |
| `integration/` | 网络隔离的 CPA 集成测试（正式套件，不属于临时代码） |
| `tools/` | 构建/辅助脚本 |
| `temp/` | 临时调试脚本、探针、一次性产物（gitignore，见下） |
| `dist/` | Docker 构建产物（gitignore） |

## 临时与测试代码

- 调试脚本、探测脚本（`*.sh`）、base64 载荷（`*.b64`）、一次性日志和探针产物，
  一律放 `temp/`，不要散落在仓库根目录。`temp/` 已被 `.gitignore` 忽略。
- 正式测试（`integration/`、`internal/**/*_test.go`）属于项目代码，保持在原位。

## 关键不变量（改代码前必读）

- **认证记录 ID 以物理文件为准**：CPA 的文件扫描（`auth.parse`）和
  `host.auth.save` 都按认证文件的相对路径推导记录 ID。插件在 `auth.parse`、
  命令行导入、刷新、模型发现更新中**不得**返回自己的内容哈希 ID，否则两条
  ID 互不匹配，`host.auth.save` 会为同一文件重复注册记录（v0.7.6 修复的
  凭证重复 bug 就是这个）。凭据内容的 `auth_id` 只用于插件内部统计，
  不要把宿主记录 ID 写进凭据 JSON。
- 所有上游 HTTP 必须走宿主桥（`host.http.do` / `host.http.do_stream`），
  插件内不得直连网络。
- 401/403 → 刷新一次并重试；402/429 → 上抛给 CPA 做冷却/切换；绝不打印
  Authorization 头或凭据内容。
- 凭据、Management Key、服务器密码、OAuth 回调 URL 不进 Git、日志、测试夹具。

## 构建与部署

改了 `console-ui/src` 必须先重建控制台再编译 `.so`（index.html 是 embed 进去的）：

```powershell
cd internal/provider/console-ui && npm run build
Copy-Item dist\index.html ..\console\index.html -Force
```

`.so` 用 Docker 构建（同时执行 gofmt / go vet / go test，Docker Desktop 需在运行）：

```powershell
$cfg = Join-Path $env:TEMP ("kp_dockercfg_" + [guid]::NewGuid().ToString("N"))
New-Item -ItemType Directory -Path $cfg | Out-Null
$env:DOCKER_CONFIG = $cfg
docker build --output type=local,dest=dist .
```

发版时同步三处版本号：`internal/provider/types.go` 的 `pluginVersion`、
`Dockerfile` 产物名、README 的版本说明。

服务器部署按 `docs/knowledge.md` 的检查清单执行；**服务器部署与 Git 推送分开**，
未明确要求时不自动更新服务器。

## 当前工作树

包含未提交的历史开发改动（v0.6–v0.7.x 大部分成果只在工作树里），
**禁止 `git reset --hard` 清理**。细节与已知事项见 `docs/knowledge.md`。

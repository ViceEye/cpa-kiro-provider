# kiro-provider 交接文档（cline 集成进行中）

更新时间：2026-08-30（额度耗尽前紧急固化）

## 一句话状态

正在给 kiro-provider 增加 **Cline 免费额度接入**（单插件复用方案），
代码在 `feat/cline-provider` 分支，**尚未完成、未合并 main、生产未部署**。
生产服务器仍运行已验证的 v0.7.7（无 cline 功能）。

## 分支与生产状态

| 位置 | 状态 |
| --- | --- |
| `main` | 干净，= 已发布的 v0.7.7 稳定版 |
| `feat/cline-provider` | cline 集成 WIP（两个提交：`f811f1e` 完整首版 + 最新 kind-marker WIP） |
| 服务器 `cli-proxy-api` | 跑 v0.7.8 测试版 .so（含 cline 但**不工作**，见下），生产语义等同 v0.7.7 |
| 服务器 `9router` | **已停止**（用户要求），compose 在 `/root/9router`，数据卷 `9router-data` 保留 |

## 已验证的技术事实（照抄可用，勿重复研究）

Cline 上游（全部实测过，2026-08-29/30）：

- 鉴权：`Authorization: Bearer workos:<access_token>`（必须 workos: 前缀）
  + 身份头 `HTTP-Referer: https://cline.bot` / `X-Title: Cline`
- 对话：`POST https://api.cline.bot/api/v1/chat/completions`（注意有 `/api`），
  OpenAI 格式 body；**响应包在 `{"data":{...choices...}}` 信封里，必须拆包**
  （9Router 的"empty response content" bug 就是没拆这个）
- 刷新：`POST /api/v1/auth/refresh`，body
  `{refreshToken, grantType:"refresh_token", clientType:"extension"}`
- 模型目录：`GET /api/v1/models`（396 个，公开）
- 额度：`GET /api/v1/users/me` → `{data:{id}}`，再 `GET /api/v1/users/{id}/balance`
  → `{data:{balance}}`（该账号 50 万积分）
- OAuth 登录（官方 SDK `sdk/packages/core/src/auth/cline.ts`）：
  authorize `?client_type=extension&callback_url=<cb>&redirect_uri=<cb>`，
  回调 URL 或裸 code 换 token：`POST /api/v1/auth/token`
  `{grant_type:"authorization_code", code, client_type:"extension",
  redirect_uri:<cb>, provider:<idp>}`
- 模型 ID 实测可用：`z-ai/glm-5.3-flash`（Ox Alpha，免费）

## 当前卡点（下一步从这里继续）

**CPA 按 auth 文件的 `type` 字段找插件**（`ParseAuths` → `authProviderRecord(type)`
→ 匹配 `auth.identifier`，即本插件的 "kiro"）。所以 cline 凭据的
`type` 必须是 `kiro`，内部用 `kind: "cline"` 字段做分发标记；模型注册
`kiro/z-ai/glm-5.3-flash`（前缀必须是 `kiro/`）。

WIP 提交已完成（需 gofmt/vet 验证编译）：
- `internal/cline/types.go`：credential 加了 `Kind` 字段
- `internal/cline/oauth.go`：Type=pluginProvider, Kind=TypeMarker
- `internal/cline/auth.go`：decode 默认 Type=pluginProvider
- `internal/cline/models.go` / `executor.go`：前缀切换到 `kiro/`
- `internal/provider/credential_type.go`：credentialTypeMarker 优先认 Kind
- 测试前缀已改

**还没做的**：
1. 上传服务器构建验证（`go vet` + `go test` 会暴露剩余编译错误）
2. 服务器上的 `cline-free.json` 要改成 `{"type":"kiro","kind":"cline",...}`
   （当前是 `type:cline`，这是模型没注册出来的根因）
3. 端到端验证：模型出现在 /v1/models → 对话成功 → 额度显示
4. 通过后：更新 README/handover、按 release checklist 升版本号（建议 0.8.0，
   cline 是新功能）、构建正式 .so、走 `temp/deploy.py`（改成新版号）部署
5. 征求用户同意后合并 main

## 构建/部署速查（额度不够时照此操作）

本机 Docker Desktop 可能没开 → **在服务器上构建**（已验证可行）：

```bash
# 本机打包上传（Git Bash，工作目录 = 仓库根）
tar --exclude=.git --exclude=dist --exclude=temp --exclude=node_modules \
    --exclude=.codex-tmp -czf /tmp/kiro-src.tar.gz .
WINPATH=$(cygpath -w /tmp/kiro-src.tar.gz)
python - <<'EOF'
import paramiko
cli = paramiko.SSHClient(); cli.set_missing_host_key_policy(paramiko.AutoAddPolicy())
cli.connect("ai.venja.cc", username="root", password="q2333vv.", timeout=20)
sftp = cli.open_sftp(); sftp.put(r"<把 WINPATH 填这里>", "/tmp/kiro-src.tar.gz")
sftp.close(); cli.close()
EOF

# 服务器构建（vet+test+build 一体）
python temp/ssh_run.py "rm -rf /tmp/kiro-build /tmp/kiro-out && mkdir -p /tmp/kiro-build /tmp/kiro-out && tar -xzf /tmp/kiro-src.tar.gz -C /tmp/kiro-build && docker run --rm -v /tmp/kiro-build:/src -w /src -v /tmp/kiro-out:/out golang:1.26-bookworm sh -c 'gofmt -w cmd/kiro-provider/*.go internal/*/*.go && go vet ./... && go test ./... && CGO_ENABLED=1 GOOS=linux GOARCH=amd64 go build -buildvcs=false -trimpath -buildmode=c-shared -ldflags=\"-s -w\" -o /out/kiro-provider-v0.7.8.so ./cmd/kiro-provider && rm -f /out/*.h && echo BUILD_OK'"

# 部署：cp /tmp/kiro-out/*.so /root/CLIProxyAPI/plugins/ && docker restart cli-proxy-api
# 验证：日志 plugin registered；/v1/models 出现 cline 模型；对话测试
```

服务器速查：IP ai.venja.cc / root / q2333vv.；CPA 管理 key `adminkazi233.`
（bcrypt 存配置，明文只在这里）；CPA API key `sk-j6w9lyPZJL1Hr6oKZ`；
cline token 备份在 9router 数据卷 sqlite（providerConnections 表）。

## 工作区其他状态（都稳定，别动）

- **9router fork**（ViceEye/9router）：master 干净稳定（cline 动态目录/额度 +
  nvidia 目录）；`feat/model-names-live-refresh` 分支有自定义显示名 +
  手动拉取上游功能（镜像已构建未部署）；zcode 实验 revert 完毕，
  备份分支 `backup/master-with-zcode` / `test/zcode-integration` 保留
- **zcode 逆向成果**（z.ai 计划池 JWT 解密、端点分析）在
  `Codex/zcode-reverse-engineer/temp/zcode-proxy/tokens.json` 和
  `9router/temp/`，接 zcode 时直接用
- 用户规则：**动代码先建分支，说 merge 才合 main；服务器部署需明确指令；
  测试脚本放 temp/（gitignored）；不动 9router（已停）**

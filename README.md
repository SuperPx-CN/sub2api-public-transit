# Sub2API 公开资料出口增强版

本仓库基于 [Wei-Shaw/sub2api](https://github.com/Wei-Shaw/sub2api) `v0.1.175` 适配，在保持原有网关、计费、账号管理和部署方式的基础上，增加面向中转站站长、PriceAI 与第三方采集器的标准化公开资料出口。

它不会开放管理后台或账号池，而是把适合公开的站点信息整理为稳定、机器可读的 `ai-transit.v1` 快照。

> 上游项目与版权归原作者及贡献者所有。本增强版只维护公开资料出口相关适配；使用前请同时阅读上游项目的风险提示与许可证。

## 相比官方版新增的能力

- 公开发现接口：`/.well-known/ai-transit.json`
- 公开快照接口：`/api/public/transit/v1/snapshot`
- 可选公开页面：`/public/transit`
- 后台双开关：分别控制机器接口与公开页面
- 分组信息：充值倍率、分组倍率、平台和模型数量
- 模型价格：输入、输出、缓存写入、缓存读取、按次和图片尺寸价格
- 缓存统计：最近 24 小时、最近 7 天和累计缓存使用
- 可用性监测：兼容 Channel Monitor v1 主动探测和 v2 被动聚合

## 隐私边界

公开资料出口采用字段白名单，只输出标准化聚合信息。以下内容不会公开：

- 上游账号、Cookie、Access Token、Refresh Token
- API Key、密钥、代理和内部连接配置
- 内部渠道 ID、账号池调度与风控细节
- 用户身份、余额、请求日志和错误详情
- Channel Monitor v2 的用户维度和吞吐量数据

没有监控样本时，接口返回空监控列表或空字段，不会把“无数据”伪装为“正常”。

## 开关

在 `系统设置 -> 功能开关 -> 公开资料出口` 中配置：

- `public_transit_enabled`：机器可读接口开关，默认开启。
- `public_transit_page_enabled`：公开页面开关，默认关闭。

关闭接口开关时，发现接口、快照接口和公开页面都会关闭。只希望 PriceAI 自动采集时，开启接口即可，无需开放页面。

## 接口

### 发现接口

```bash
curl https://your-domain.example/.well-known/ai-transit.json
```

```json
{
  "schema_version": "ai-transit.v1",
  "system": "sub2api",
  "snapshot_url": "https://your-domain.example/api/public/transit/v1/snapshot",
  "homepage_url": "https://your-domain.example/public/transit",
  "generated_at": "2026-08-12T00:00:00Z"
}
```

`homepage_url` 只在公开页面开关开启时返回。

### 快照接口

```bash
curl https://your-domain.example/api/public/transit/v1/snapshot
```

快照包含站点基础信息、计费说明、公开分组、模型价格、缓存统计、可用性摘要、数据完整度和公开端点。账号、密钥及用户数据不在响应结构中。

### 公开页面

```text
https://your-domain.example/public/transit
```

页面开关默认关闭。开启后访客可以直接查看价格、倍率、缓存与监控摘要。

## 部署

本增强版沿用官方 Sub2API 的部署和升级前置条件：

- Linux `amd64` 或 `arm64`
- PostgreSQL 15+
- Redis 7+
- Docker，或官方支持的二进制部署环境

### 预构建镜像（仅 linux/amd64）

本仓库通过 GitHub Actions 发布到 `ghcr.io/superpx-cn/sub2api-public-transit`。预构建镜像仅支持 `linux/amd64`；`arm64` 环境需自行构建。

```bash
# 最新一次由正式版本标签推送发布的镜像
docker pull ghcr.io/superpx-cn/sub2api-public-transit:latest

# 指定已发布版本（示例版本号，请替换为实际版本）
docker pull ghcr.io/superpx-cn/sub2api-public-transit:1.2.3

# 指定源码提交（将变量替换为完整的 40 位提交 SHA）
COMMIT_SHA=替换为完整提交SHA
docker pull "ghcr.io/superpx-cn/sub2api-public-transit:sha-${COMMIT_SHA}"
```

在现有 Compose 配置中将应用服务的 `image` 改为所需镜像标签，沿用原有 PostgreSQL、Redis、环境变量与持久化卷配置。

### 发布与手动重建

`.github/workflows/release.yml` 仅响应 `v*` 标签推送和手动触发，普通分支推送与 PR 不发布镜像。发布前须先将此工作流合入默认分支；新版本标签应指向包含此工作流的提交。

- 自动发布：创建并推送 `vX.Y.Z` 或预发布标签（如 `v1.2.3-rc.1`）。版本数字不得带多余前导零；不支持 `+build` 元数据。去掉 `v` 后的镜像标签最多 128 个字符。
- 手动重建：进入 **Actions → Release → Run workflow**，选择默认分支并在必填的 `tag` 中填写已有版本标签（如 `v1.2.3`）。实际构建源码来自该标签，而不是所选分支。标签不存在或格式错误时会在构建前失败。
- 两种入口都发布 `:<去掉 v 的版本号>` 和 `:sha-<实际检出提交的完整 SHA>`；同一版本的自动发布与手动重建串行执行，重建会覆盖对应标签。
- 只有正式版本的标签推送发布成功后才更新 `:latest`。预发布和所有手动重建都不更新 `:latest`；它表示最近成功自动发布的正式版本，不按版本号比较大小。

工作流在 Ubuntu x86 runner 上使用根目录 Dockerfile 构建前端和嵌入前端的 Go 后端，使用 GitHub Actions 构建层缓存，并注入去掉 `v` 的版本号、标签提交 SHA 和 UTC 构建时间。镜像附带 OCI source、revision、version 和 created 标签；source 指向当前仓库。发布路径按当前仓库路径转为小写生成，因此 fork 会发布到自己的 GHCR 路径。

发布使用内置 `GITHUB_TOKEN`，仅申请 `contents: read` 和 `packages: write`，无需 Docker Hub 凭据。此工作流只发布容器镜像，不创建 GitHub Release、不上传二进制附件、不回写 VERSION 或其他仓库文件，也不自动部署。

**首次发布后**：GHCR 新建的包默认私有。仓库管理员需打开 GitHub 仓库的 **Packages → sub2api-public-transit → Package settings → Change visibility**，将包设为 **Public**，才能匿名拉取。保留私有时，拉取前需用具有该包读取权限和 `read:packages` scope 的 personal access token (classic) 登录 `ghcr.io`。

首次 Actions 发布完成后，检查构建步骤报告的 digest 和 GHCR 包内版本/SHA 标签；正式自动发布还应检查 `latest` 指向同一 digest。拉取后可验证架构、来源与版本（替换示例版本号）：

```bash
docker image inspect ghcr.io/superpx-cn/sub2api-public-transit:1.2.3 \
  --format '{{.Os}}/{{.Architecture}} {{index .Config.Labels "org.opencontainers.image.source"}}'
docker run --rm ghcr.io/superpx-cn/sub2api-public-transit:1.2.3 /app/sub2api -version
```

### 本地构建

构建当前主机架构的 Docker 镜像：

```bash
docker build -t sub2api-public-transit:0.1.175 .
```

已有 Sub2API 站点升级前，请先备份数据库、配置与部署文件，并在独立分支或预发布环境验证。不要用本仓库直接覆盖存在未提交二改的工作区。

## 验证

```bash
pnpm --dir frontend install --frozen-lockfile
pnpm --dir frontend run build

cd backend
go test ./internal/service ./internal/handler ./internal/server ./internal/repository ./internal/web -tags embed
```

启动服务后检查：

```bash
curl -I https://your-domain.example/public/transit
curl https://your-domain.example/.well-known/ai-transit.json
curl https://your-domain.example/api/public/transit/v1/snapshot
```

## 给 PriceAI 的站点准入建议

- `/.well-known/ai-transit.json` 与快照接口可公开访问
- 充值倍率、分组倍率和模型价格来自真实可用配置
- 可用性监测保持启用，并允许无样本状态如实呈现
- 不在其他自定义公开字段中拼入账号、密钥、Cookie 或内部渠道 ID

## 与原版 Sub2API 的关系

本仓库不是一个重新设计的网关，也不改变 Sub2API 的核心定位。官方版负责通用网关能力，本增强版只增加独立的公开输出层，供站长自愿披露价格和运行状态。

当官方版本更新时，应以新官方版本为基线重新适配公开输出层，不能直接用官方分支覆盖本仓库，否则增强功能会丢失。

## 许可证

本仓库继承上游项目的许可证和提交历史。详见 [LICENSE](LICENSE)。

更完整的字段与接入说明见 [README_PUBLIC_TRANSIT_CN.md](README_PUBLIC_TRANSIT_CN.md)。

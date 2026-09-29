# Helm 部署指南

本 Chart 只部署 Sub2API 公开资料出口增强版应用，连接**已有的 PostgreSQL 和 Redis**，不会创建数据库、Redis、Secret 或子 Chart。Chart 位于 [helm/sub2api-public-transit](helm/sub2api-public-transit)，初始版本为 0.1.0（Helm v2 application Chart）。

固定一个副本，使用 `Recreate` 策略；升级、重启、节点迁移都可能短暂停机，并中断正在进行的 SSE/WebSocket 请求。默认将 `/app/data` 挂载到 1Gi、ReadWriteOnce 的 PVC。此版本不提供多副本、HPA、宿主机 `datamanagementd` 或 Chart 发布流程。

## 1. 准备依赖

- Helm 3，kubectl，以及可用的 Kubernetes 集群（API 最低兼容 1.23；请使用仍受维护的集群版本）。本仓库 CI 固定 Helm 3.21.4。
- PostgreSQL 15+、Redis 7+；应用 Pod 必须能通过 DNS、网络策略、防火墙连接它们。Redis 使用单实例连接地址，不支持此 Chart 自动配置 Sentinel/Cluster。
- 支持 ReadWriteOnce 的 CSI/StorageClass，或同命名空间中已经准备好的 PVC。存储需支持 UID/GID 1000 读写；某些 NFS/CSI 不会应用 fsGroup，需要管理员预先设置目录权限。
- 默认调度到 `kubernetes.io/os=linux`、`kubernetes.io/arch=amd64`。官方增强版预构建镜像地址为 `ghcr.io/superpx-cn/sub2api-public-transit`；需自己选定已发布 tag 或 digest，Chart 不默认使用 latest，也不会检查 tag 是否真实存在。
- 使用 Ingress 时，预先安装并配置 Ingress Controller、DNS 和 TLS Secret；Chart 不安装 Controller 或 cert-manager。

### PostgreSQL 初始化权限

自动初始化先连接维护库 `postgres`，再检查/创建目标数据库。因此，即使目标库已存在，账号也必须能连接 `postgres`。目标库可以由 DBA 预先创建；否则应用账号需有 `CREATEDB` 权限。该账号还需拥有目标 schema 以及执行项目迁移所需的建表、索引、外键、序列和数据读写权限。推荐专用账号拥有目标库和 public schema 中的应用对象，不要使用只读或仅 DML 账号运行迁移。

以下 SQL 由 DBA 按实际库名/角色执行；角色密码通过组织现有的凭据管理流程设置：

```sql
-- 假设 sub2api 登录角色已创建并设置密码。
GRANT CONNECT ON DATABASE postgres TO sub2api;
CREATE DATABASE sub2api OWNER sub2api;
-- 连接 sub2api 数据库后执行：
GRANT USAGE, CREATE ON SCHEMA public TO sub2api;
```

已有对象的所有权也必须满足迁移需求。仅授予 schema 的 CREATE 并不能让应用修改其他角色拥有的表。迁移可能耗时较长，参考后文调整启动预算。

## 2. 创建 Secret

所有操作在仓库根目录执行。示例统一使用命名空间和 release 名 `transit`，并在 values 中设置 `fullnameOverride: transit`，使资源名也为 `transit`。多实例需分别使用命名空间或不同资源名。

先创建命名空间：

```bash
kubectl create namespace transit
```

以下 Bash 示例从交互输入读取数据库密码和管理员初始密码，将密钥放在权限受限的临时文件中；不要将这些内容提交到 Git，也不要启用 shell 跟踪。应保存生成的 JWT/TOTP 密钥到组织的密码库以便恢复。Redis 无认证时无需提供 REDIS_PASSWORD；开启认证时需添加它。

```bash
(
  set -eu
  umask 077
  secret_dir=$(mktemp -d)
  trap 'rm -rf "$secret_dir"' EXIT
  read -rsp 'PostgreSQL 密码: ' database_password; printf '\n'
  read -rsp '管理员初始密码（使用强密码）: ' admin_password; printf '\n'
  test -n "$database_password" && test -n "$admin_password"
  printf '%s' "$database_password" > "$secret_dir/DATABASE_PASSWORD"
  printf '%s' "$admin_password" > "$secret_dir/ADMIN_PASSWORD"
  openssl rand -hex 32 | tr -d '\n' > "$secret_dir/JWT_SECRET"
  openssl rand -hex 32 | tr -d '\n' > "$secret_dir/TOTP_ENCRYPTION_KEY"
  # 如 Redis 开启认证，取消下列三行注释：
  # read -rsp 'Redis 密码: ' redis_password; printf '\n'
  # test -n "$redis_password"
  # printf '%s' "$redis_password" > "$secret_dir/REDIS_PASSWORD"
  kubectl -n transit create secret generic transit-secrets --from-file="$secret_dir"
)
```

Chart 通过 `secretKeyRef` 注入以下变量，**不生成、不保存密码到 Helm values 或模板**：

| 变量 | Secret 引用位置 | 默认 key |
| --- | --- | --- |
| DATABASE_PASSWORD | externalDatabase.existingSecret | DATABASE_PASSWORD |
| JWT_SECRET | app.existingSecret | JWT_SECRET |
| TOTP_ENCRYPTION_KEY | app.existingSecret | TOTP_ENCRYPTION_KEY |
| ADMIN_PASSWORD | app.existingSecret | ADMIN_PASSWORD |
| REDIS_PASSWORD（开启认证时） | externalRedis.auth.existingSecret | REDIS_PASSWORD |

可通过 `passwordKey` 和 `app.secretKeys` 映射已有 Secret 的不同 key。Secret 必须与应用同命名空间、包含非空有效值；离线渲染只能检查引用是否填写，不能确认 Secret 是否存在或内容是否正确。JWT 至少 32 字节；TOTP 使用 `openssl rand -hex 32` 生成的 64 位十六进制密钥。固定并备份 JWT/TOTP 密钥，避免会话失效或已有 2FA 无法解密。ADMIN_PASSWORD 仅用于初次创建管理员，修改此 Secret 不会重置现有管理员密码。

## 3. 配置、渲染和安装

复制[最小 values 示例](helm/examples/values-minimal.yaml)：

```bash
cp deploy/helm/examples/values-minimal.yaml /tmp/transit-values.yaml
# 编辑 /tmp/transit-values.yaml：替换 tag、外部数据库/Redis 地址和管理员邮箱。
helm lint --strict deploy/helm/sub2api-public-transit -f /tmp/transit-values.yaml
helm template transit deploy/helm/sub2api-public-transit \
  --namespace transit -f /tmp/transit-values.yaml > /tmp/transit-rendered.yaml
helm upgrade --install transit deploy/helm/sub2api-public-transit \
  --namespace transit -f /tmp/transit-values.yaml --wait --timeout 15m
```

`REPLACE_WITH_PUBLISHED_TAG` 仅是可渲染的占位符，必须替换成实际镜像版本才能运行。缺少 image tag/digest、数据库/Redis host、必需 Secret 引用时渲染会失败。也可使用 `image.digest: sha256:<64位十六进制摘要>` 固定镜像；同时配置 tag 和 digest 时 digest 优先，升级 tag 前须清空旧 digest。

安装使用环境变量并固定 `AUTO_SETUP=true`、`SERVER_HOST=0.0.0.0`、`SERVER_PORT=8080`、`DATA_DIR=/app/data`。应用自行写入 `/app/data/config.yaml` 和安装标记；不要在此路径挂载只读配置。应用写入的配置文件可能包含数据库、Redis、JWT 等敏感信息，PVC 和备份都按密钥资料保护。

### 常用参数

完整选项见 [values.yaml](helm/sub2api-public-transit/values.yaml)，参数类型由 [values.schema.json](helm/sub2api-public-transit/values.schema.json) 校验。

| 参数 | 默认/用途 |
| --- | --- |
| externalDatabase.port / username / database | 5432 / sub2api / sub2api |
| externalDatabase.sslmode | disable；支持 require、verify-ca、verify-full |
| externalRedis.port / username / database | 6379 / 空 / 0 |
| externalRedis.enableTLS / auth.enabled | false / false；生产环境按服务端配置开启 |
| app.timezone | Asia/Shanghai |
| service.type / port / annotations | ClusterIP / 8080 / {} |
| persistence.size | 1Gi；只接受正整数 Mi/Gi/Ti |
| persistence.storageClass | 空字符串省略字段、使用集群默认；`-` 显式指定无 StorageClass |
| persistence.existingClaim | 非空时仅引用已有 PVC，不创建 PVC |
| persistence.retain | true；Chart 创建的 PVC 带 keep 注解 |
| resources | 默认 {}；上线前根据容量测试配置 requests/limits |
| nodeSelector / tolerations / affinity | Linux amd64 / [] / {} |
| probes.startup.failureThreshold / periodSeconds | 60 / 10，默认允许约十分钟启动 |
| terminationGracePeriodSeconds | 60；不保证所有长请求都能完成 |
| extraEnv / extraVolumes / extraVolumeMounts | 原生 EnvVar / Volume / VolumeMount 列表 |

示例调度与资源覆盖（复制到自己的 values 文件）：

```yaml
resources:
  requests:
    cpu: 250m
    memory: 512Mi
  limits:
    memory: 2Gi
probes:
  startup:
    failureThreshold: 120
extraEnv:
  - name: SETUP_MIGRATION_TIMEOUT_SECONDS
    value: "900"
```

这些资源数值仅为起点，不代表吞吐保证。首次自动初始化的迁移超时与 Kubernetes 启动探针预算是两个配置，需一并考虑；扩大启动探针预算时也扩大 Helm 的 `--timeout`。Deployment 进度期限随启动探针预算调整，额外留出五分钟，避免默认十分钟期限提前报失败。三个探针固定访问 `/health`，可调整周期、超时和失败次数。Chart 固定 UID/GID/fsGroup 1000，禁止提权，移除 capabilities，使用 RuntimeDefault seccomp，不挂载 ServiceAccount token。

extraEnv 中的值须是字符串；额外密钥使用 `valueFrom.secretKeyRef`。禁止覆盖 Chart 管理的连接/密钥/端口变量或重复变量名。额外挂载使用 `/app/certs` 等独立路径，不能遮盖 `/app/data` 或其父目录。Helm 合并 values 时**列表整体替换**：叠加多个包含 extraEnv 的文件时，需手动合并为一个列表，extraVolumes/extraVolumeMounts 同理。

### 私有仓库与 arm64

先准备同命名空间的镜像拉取 Secret，再设置 `imagePullSecrets: [{name: registry-credentials}]`。Chart 不创建仓库凭据。arm64 节点需自行构建并推送镜像，例如：

```bash
docker buildx build --platform linux/arm64 \
  -t registry.example.com/team/sub2api:my-arm64-release --push .
```

同时配置自己的 `image.repository`、`image.tag`（或 digest），并将 `nodeSelector.kubernetes.io/arch` 改为 `arm64`，保持 Linux 选择器。

## 4. 外部依赖 TLS 与证书

[values-external-tls.yaml](helm/examples/values-external-tls.yaml) 展示 PostgreSQL verify-full、Redis TLS/ACL 密码认证与 CA 挂载。先准备有效 CA 和前述 REDIS_PASSWORD：

```bash
kubectl -n transit create secret generic transit-external-ca --from-file=ca.crt=/path/to/ca-bundle.pem
helm template transit deploy/helm/sub2api-public-transit --namespace transit \
  -f /tmp/transit-values.yaml -f deploy/helm/examples/values-external-tls.yaml
# 审查并替换 host、port、用户名及证书后，用同一组 -f 参数执行 upgrade --install。
```

PostgreSQL 使用 lib/pq，支持 `PGSSLROOTCERT`；Redis 使用 Go 的系统信任库，可通过 `SSL_CERT_FILE` 增加 CA bundle。证书 SAN 必须匹配连接主机名；示例使用相同 bundle，可包含多个 CA。不要通过关闭验证绕过证书错误。require 加密连接不等同于 verify-full 的主机名验证；当前应用不提供 Redis 客户端证书/mTLS 配置，不能仅靠挂载客户端证书启用 mTLS。证书轮换后也需重启应用。

## 5. 访问与 Ingress

先检查 Pod/PVC 并启动本地转发：

```bash
kubectl -n transit get pods,pvc,svc
kubectl -n transit rollout status deployment/transit --timeout=15m
kubectl -n transit logs deployment/transit --tail=100
kubectl -n transit port-forward svc/transit 8080:8080
```

另一终端执行：

```bash
curl -fsS http://127.0.0.1:8080/health
curl -fsS http://127.0.0.1:8080/ > /tmp/transit-home.html
curl -fsS http://127.0.0.1:8080/.well-known/ai-transit.json
curl -fsS http://127.0.0.1:8080/api/public/transit/v1/snapshot
```

浏览器打开首页，用初始化管理员账号登录。机器公开接口默认开启，若已在设置中关闭，接口不可用属于预期行为；公开页面 `/public/transit` 默认关闭，需在「系统设置 → 功能开关 → 公开资料出口」开启页面后再验证。只用 curl 获取 HTML 不代表 SPA 功能通过，应在浏览器检查页面、登录及公开数据是否正确。

使用 [values-ingress-tls.yaml](helm/examples/values-ingress-tls.yaml) 前，替换域名、IngressClass、实际直连代理 CIDR/IP，并创建 TLS Secret：

```bash
kubectl -n transit create secret tls transit-tls \
  --cert=/path/to/fullchain.pem --key=/path/to/privkey.pem
helm upgrade --install transit deploy/helm/sub2api-public-transit --namespace transit \
  -f /tmp/transit-values.yaml -f deploy/helm/examples/values-ingress-tls.yaml \
  --wait --timeout 15m
curl -fsS https://transit.example.com/health
```

示例 annotations 针对理解这些配置项的 NGINX Controller；其他 Controller 需使用其对应设置，并验证 WebSocket Upgrade 转发、SSE 响应/请求缓冲关闭、读写超时与 CDN/LB 超时。3600 秒仅是示例，整条代理链都要满足实际长连接时间。静态 Ingress 配置不能保证流式请求运行正常，应使用实际 API Key 发起 SSE 和 WebSocket 请求做端到端验证。

可信代理配置必须使用直接连接应用的代理 CIDR/IP，不能照抄示例网段或信任全网。示例设置 `SECURITY_TRUST_FORWARDED_IP_FOR_API_KEY_ACL=false` 和 `SERVER_TRUSTED_PROXIES`；已有安装还要检查后台持久化的同名安全开关，因为环境变量不会覆盖管理员已保存的所有设置。限制应用来源仅为可信代理，并在边缘清理伪造转发头。更多细节见 [EDGE_SECURITY.md](EDGE_SECURITY.md)。

## 6. 升级、轮换与备份

每次升级使用完整、经过审查的 values 文件列表，避免 `--reuse-values` 把旧配置悄悄保留。固定镜像版本，记录旧镜像、Chart 版本及 Helm revision；事先阅读应用迁移变更，安排停机窗口。

在维护窗口暂停写入及管理操作，备份外部 PostgreSQL、Redis（按其服务的快照/AOF策略）、PVC 文件和原有 Secret。优先采用数据库服务和 CSI 的一致性备份。以下逻辑导出供运维参考，需确认镜像内 pg_dump 支持数据库服务端版本，备份目标应受访问控制：

```bash
umask 077
kubectl -n transit exec deployment/transit -- sh -c \
  'export PGPASSWORD="$DATABASE_PASSWORD" PGSSLMODE="$DATABASE_SSLMODE"; exec pg_dump -h "$DATABASE_HOST" -p "$DATABASE_PORT" -U "$DATABASE_USER" -d "$DATABASE_DBNAME" -Fc' \
  > transit-database.dump
kubectl -n transit exec deployment/transit -- tar -C /app/data -czf - . > transit-data.tgz
kubectl -n transit get secret transit-secrets -o yaml > transit-secrets.backup.yaml
helm history transit -n transit
```

核对命令退出码并验证备份可恢复；在线 tar 和数据库导出不是跨资源原子快照，严格一致性场景需维护停写并使用平台快照/备份方案。外部证书、镜像凭据及 values 也应纳入备份。不要将未加密备份提交到仓库，Secret 的 base64 不是加密。

修改 values 中的 tag/digest 后执行第 3 节的 upgrade 命令，并带上实际使用的所有 overlay。ConfigMap 的内容校验和写入 Pod 注解，配置变更会触发 Recreate。Chart 无法感知外部 Secret 内容变化；完成凭据协调轮换后执行：

```bash
kubectl -n transit rollout restart deployment/transit
kubectl -n transit rollout status deployment/transit --timeout=15m
```

不要随意更换 TOTP 加密密钥；它与数据库中的加密记录配套，替换会破坏已有 2FA。JWT 轮换会使旧会话失效。ADMIN_PASSWORD 不用于日常密码重置。应用生成的旧 config.yaml 可能仍保存旧凭据，轮换时也应按照应用的配置和备份流程处理该文件。

### 回滚

```bash
helm history transit -n transit
# 将 1 替换为已确认兼容当前数据库结构的 revision。
helm rollback transit 1 -n transit --wait --timeout 15m
```

**Helm rollback 不会撤销数据库迁移，不恢复 PVC 内容，也不恢复外部 Secret。** 回滚前必须确认旧应用兼容当前 schema；否则停止流量，按恢复方案还原相匹配的数据库、PVC、密钥和镜像。自动回滚参数同样不能解决数据库兼容性问题。

### 卸载与复用 PVC

```bash
helm uninstall transit -n transit
kubectl -n transit get pvc transit
# 保留相同的 Secret 和数据库；重新安装引用保留下来的卷。
helm upgrade --install transit deploy/helm/sub2api-public-transit --namespace transit \
  -f /tmp/transit-values.yaml --set persistence.existingClaim=transit \
  --wait --timeout 15m
```

默认 keep 注解使 Chart 创建的 PVC 在卸载后保留；应现场确认卷还在。重新安装不要尝试直接创建同名 PVC，需用 existingClaim 复用。引用已有 PVC 时 Chart 不负责创建、权限初始化或删除它。不要删除命名空间来替代卸载，否则其中 PVC 和 Secret 也会被删除。手动删除 PVC 或设置 retain=false 可能触发底层 PV 回收，仅在备份验证后按存储策略操作。

StorageClass 和 PVC 的多数字段不可变；扩容还依赖存储类支持，不支持缩容。不要将更改 values 当成 PVC 迁移。需要新存储类时，准备新 PVC 并执行经过验证的数据恢复/迁移流程。

## 7. 故障排查与真实环境验收

| 症状 | 优先检查 |
| --- | --- |
| Helm 渲染失败 | tag/digest、外部 host、Secret 引用、schema 报错路径；不支持 replicaCount/HPA 参数 |
| ImagePullBackOff / exec format error | 镜像 tag、仓库权限、imagePullSecrets、镜像架构与节点标签 |
| PVC Pending / Pod Pending | StorageClass、容量、可用区、ReadWriteOnce 挂载占用、调度规则 |
| Permission denied | UID/GID 1000、fsGroup、CSI/NFS 权限；Chart 不用 root initContainer 修权限 |
| CreateContainerConfigError | 同命名空间中 Secret/指定 key 是否存在 |
| 自动初始化或迁移失败 | postgres 维护库 CONNECT、目标库所有权/迁移权限、DNS/TLS/密码、数据库日志 |
| Redis NOAUTH / 证书错误 | auth.enabled、用户名/密码 key、enableTLS、端口、SAN 和 CA |
| 启动超时或反复重启 | startup 探针预算、迁移耗时、应用日志及 Helm timeout；不要仅延长时间掩盖错误 |
| Ingress 502/504 / 流式中断 | readiness、Service endpoints、Controller 日志、超时/缓冲、TLS、升级停机 |
| 公开页面不可用 | 页面开关默认关闭；机器接口与页面开关分别验证 |

```bash
kubectl -n transit describe pod -l app.kubernetes.io/instance=transit
kubectl -n transit describe pvc transit
kubectl -n transit get events --sort-by=.lastTimestamp
kubectl -n transit logs deployment/transit --tail=200
```

真实环境验收需要实际执行并记录：安装成功、Pod Ready、PVC Bound；第 5 节页面和公开接口验证；有效凭据的 SSE/WebSocket 测试；Pod 重建后配置、安装状态及业务数据仍存在。可在维护窗口使用无业务意义的标记检查卷持久化：

```bash
kubectl -n transit exec deployment/transit -- sh -c 'date -u > /app/data/helm-persistence-smoke'
kubectl -n transit exec deployment/transit -- cat /app/data/helm-persistence-smoke
kubectl -n transit get pods -l app.kubernetes.io/instance=transit -o wide
kubectl -n transit rollout restart deployment/transit
kubectl -n transit rollout status deployment/transit --timeout=15m
kubectl -n transit get pods -l app.kubernetes.io/instance=transit -o wide
kubectl -n transit exec deployment/transit -- cat /app/data/helm-persistence-smoke
```

确认 Pod 已替换、标记内容一致，再验证管理员登录、配置和公开接口。重建会中断请求。本次 Chart 交付的自动化验证只覆盖离线 lint/render 与结构断言，**不表示已完成集群安装、PVC 驱动或应用运行验证**。

## 8. 本地自动化校验

安装 Helm 3 和 Python 3/PyYAML 后，在仓库根目录运行：

```bash
python3 -m venv /tmp/transit-helm-test-venv
/tmp/transit-helm-test-venv/bin/pip install PyYAML==6.0.2
PATH="/tmp/transit-helm-test-venv/bin:$PATH" sh deploy/tests/helm-chart-test.sh
```

脚本对基础配置、所有可复制示例、Ingress/TLS、外部 TLS/Redis 认证、已有 PVC、StorageClass、私有镜像/digest、环境变量、挂载、安全上下文、更新策略及错误配置执行 lint/template 与 YAML 结构断言；CI 的 helm job 执行同一脚本。校验不连接集群、不拉取应用镜像，也不证明外部依赖能连通。

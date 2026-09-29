# 部署与旧版本迁移

这是独立只读资料服务。仅连接已有外部 PostgreSQL，不创建业务数据库、不迁移、不写业务表，不连接 Redis。Chart 版本为 0.2.1；默认一个副本，纯 Go 镜像以 UID/GID 10001 运行，根文件系统可设为只读，无 PVC 和 /app/data 要求。

## 数据库授权

以下是供数据库管理员审阅并手动执行的授权示例，应用不会运行它们。将数据库名、账号和密码替换为实际值。只读角色不得是超级用户、表所有者，也不得继承写入角色。不要复用原 Sub2API 管理账号。源库 settings 表可能含其他配置，服务只按固定 key 白名单读取；数据库管理员仍须妥善限制该账号的持有范围。

~~~sql
CREATE ROLE transit_reader LOGIN PASSWORD 'replace-with-strong-password'
  NOSUPERUSER NOCREATEDB NOCREATEROLE NOINHERIT;
GRANT CONNECT ON DATABASE sub2api TO transit_reader;
GRANT USAGE ON SCHEMA public TO transit_reader;
ALTER ROLE transit_reader SET default_transaction_read_only = on;

GRANT SELECT (key, value) ON settings TO transit_reader;
GRANT SELECT (id, name, platform, subscription_type, rate_multiplier,
  image_price_1k, image_price_2k, image_price_4k,
  status, is_exclusive, deleted_at) ON groups TO transit_reader;
GRANT SELECT (id, name, status, model_mapping) ON channels TO transit_reader;
GRANT SELECT (id, channel_id, group_id) ON channel_groups TO transit_reader;
GRANT SELECT (id, channel_id, platform, models, billing_mode, input_price,
  output_price, cache_write_price, cache_read_price, image_output_price,
  per_request_price) ON channel_model_pricing TO transit_reader;
GRANT SELECT (id, pricing_id, min_tokens, max_tokens, tier_label, input_price,
  output_price, cache_write_price, cache_read_price, per_request_price,
  sort_order) ON channel_pricing_intervals TO transit_reader;
GRANT SELECT (group_id, created_at, input_tokens, cache_creation_tokens,
  cache_read_tokens) ON usage_logs TO transit_reader;
~~~

分组模型配置列单独授权。先在目标数据库、使用应用相同的 `search_path` 检查实际字段：

~~~sql
SELECT attname, format_type(atttypid, atttypmod) AS column_type
FROM pg_catalog.pg_attribute
WHERE attrelid = 'groups'::regclass AND attnum > 0 AND NOT attisdropped
  AND attname IN ('model_allowlist', 'models_list_config')
ORDER BY CASE attname WHEN 'model_allowlist' THEN 0 ELSE 1 END;
~~~

仅执行与结果匹配的一条授权；不要对不存在的列执行 GRANT：

~~~sql
-- 存在新列时（Sub2API v0.2.8 已包含）：只授权新列，包括两列并存情况。
GRANT SELECT (model_allowlist) ON groups TO transit_reader;

-- 仅存在旧列时：改为执行下面这条，不执行上面的新列授权。
-- GRANT SELECT (models_list_config) ON groups TO transit_reader;
~~~

两列均不存在时无需配置列授权，仍返回渠道/定价目录并在 `completeness.warnings` 告警。不要为本服务补建旧列。字段存在但未授权、字段类型不是 JSON/JSONB，或公开分组的配置无法解析时，两个接口均返回 503；不会绕过新列改读旧列。两列并存且新列关闭、为 `{}` 或 NULL 时仍只按新列处理。

只对实际存在且需要公开的监控表授权；缺失的可选表会产生完整度告警，不要求创建它们：

~~~sql
-- v1：刻意不授予 endpoint、api_key_encrypted、message 或请求模板字段。
GRANT SELECT (id, name, provider, group_name, primary_model, extra_models,
  enabled) ON channel_monitors TO transit_reader;
GRANT SELECT (id, monitor_id, model, status, latency_ms, ping_latency_ms,
  checked_at) ON channel_monitor_histories TO transit_reader;
-- v2：不读取用户级 rollup。
GRANT SELECT (id, enabled, platforms, group_ids, health_thresholds,
  ignored_error_categories) ON channel_monitor_v2_config TO transit_reader;
GRANT SELECT (id, usage_coverage_start, error_coverage_start, data_through,
  last_successful_at) ON channel_monitor_v2_watermarks TO transit_reader;
GRANT SELECT (bucket_seconds, bucket_start, platform, group_id, model,
  success_requests, error_requests, input_tokens, cache_creation_tokens,
  cache_read_tokens, ttft_sum_ms, ttft_count)
  ON channel_monitor_v2_metrics_rollup TO transit_reader;
GRANT SELECT (bucket_seconds, bucket_start, platform, group_id, model,
  user_id, metric, upper_bound_ms, sample_count)
  ON channel_monitor_v2_latency_histograms_rollup TO transit_reader;
GRANT SELECT (bucket_seconds, bucket_start, platform, group_id, model,
  error_category, error_requests)
  ON channel_monitor_v2_error_metrics_rollup TO transit_reader;
~~~

v2 histogram 仅使用 user_id=0 的公共聚合，不输出内部维度 ID。accounts、users、支付配置/凭据表、账号凭据、调度表均不需要授权。连接默认只读与事务只读是额外保护；真正的权限边界仍是数据库角色授权。

## 数据结构基线

本项目版本 **0.2.1** 与上游 Sub2API 版本是两条独立版本线，兼容起点为 **Sub2API v0.2.8**（该版本已包含新列及迁移 235/236）。核心表及所需列以以上基础 GRANT 清单为准；可选配置列按 `model_allowlist` → `models_list_config` → 无配置选择，JSON/JSONB 结构均为 `{enabled,models}`。`model_mapping={platform:{source:target}}`；`pricing.models` 为字符串数组。groups.status='active'、is_exclusive=false、deleted_at IS NULL 才能公开。settings 只读取 public_transit_enabled、site_name、contact_info、MIN_RECHARGE_AMOUNT、BALANCE_RECHARGE_MULTIPLIER、channel_monitor_enabled、channel_monitor_mode。

旧配置仅补充分组目录；新配置开启时按 v0.2.8 的白名单匹配规则筛选现有渠道/定价候选，匹配客户端可见模型名，保留价格和来源。启用但列表为空时输出空数组，不生成通配模型或未确认支持的模型。模型别名范围与已核实来源见 [兼容性记录](../docs/sub2api-compatibility.md)。每次请求重新探测配置字段；已生成快照可在 60 秒 TTL 内保持旧值，到期刷新后采用新结构，无需重启。

监控 v1 要求 channel_monitors 与 channel_monitor_histories；v2 要求 config、metrics_rollup、latency_histograms_rollup、error_metrics_rollup、watermarks。v2 使用 bucket_seconds=43200 的既有结果，依赖源 Sub2API 自行生成；本服务不会启动聚合器或更新水位。只有 1m 表、没有对应 rollup 的旧库会得到完整度告警。现有表的缺列、类型不匹配或权限错误返回 503。不要用测试 fixture“修补”生产库。

## Docker Compose

~~~sh
cp deploy/.env.example deploy/.env
# 编辑所有连接参数，指定已发布的 IMAGE_TAG 和 PUBLIC_BASE_URL
cd deploy
docker compose up -d
~~~

示例只包含 transit 服务，没有内置 PostgreSQL 或 Redis。端口默认绑定 127.0.0.1:8080，由运维提供 TLS 反向代理；代理不应增加旧页面或网关路由。镜像内 CA 证书支持 PostgreSQL TLS。若使用 verify-full，自定义 CA 通过只读挂载配合 DATABASE_SSLROOTCERT。

## Helm

在目标 namespace 预先创建仅含 DATABASE_PASSWORD 的 Secret（例如 transit-postgres）。无需管理员、JWT、TOTP、Redis Secret。

~~~sh
helm lint deploy/helm/sub2api-public-transit \
  -f deploy/helm/examples/values-minimal.yaml
helm template transit deploy/helm/sub2api-public-transit \
  -f deploy/helm/examples/values-ingress-tls.yaml
# 审核渲染结果后，由运维执行安装或升级：
helm upgrade --install transit deploy/helm/sub2api-public-transit \
  --namespace transit --create-namespace -f your-values.yaml
~~~

image.tag 或 image.digest 必填，继续使用 ghcr.io/superpx-cn/sub2api-public-transit。镜像按现有仓库标签流程发布。Service、Ingress、资源 requests/limits、nodeSelector、tolerations 和 affinity 可配置。extraVolumes/extraVolumeMounts 用于只读 CA 或本地价格文件；extraEnv 可设置 DATABASE_SSLROOTCERT、PRICING_FILE，不可覆盖受 Chart 管理的变量。

startup/readiness/liveness 均为 TCP 探针，故探针只说明 HTTP 进程在监听，数据库故障需由调用两个公开接口的外部可用性检查发现。服务不会暴露 /health。

## 从旧部署迁移

1. 保存现有 values、Secret 引用和 PVC 名称，确认新的只读角色和上述表结构；先渲染 0.2.1 的部署并审核。
2. 新建最小 values，指定外部 PostgreSQL、PUBLIC_BASE_URL 和数据库密码 Secret；删除 externalRedis、app 管理凭据、persistence 等旧配置。不要使用 --reuse-values 将全部旧参数带入。
3. 需要原站页面链接时显式设置 STATION_*_URL；更新采集方为两个标准接口。旧别名、/public/transit、/health、所有认证和网关路径永久移除。
4. 检查旧 release 中 PVC 的 helm.sh/resource-policy。如果旧 PVC 没有 keep 策略，应先由运维制定保留方案或使用新的 release 名并行切换，避免 Helm 删除旧 release 中已不再渲染的资源。新 Chart 不包含删除 PVC、Secret 或数据库的 hook。
5. 由运维部署并验证 JSON、404/405、只读权限、数据库故障时的 503，再切换采集流量。
6. 旧 PVC、管理员/Redis Secret 和数据库由运维单独决定保留或清理。本仓库的升级脚本不会自动删除它们。本次代码改造不执行现网部署或操作共享数据库/Redis。

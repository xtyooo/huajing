# New API 生产升级手册

## 一、适用范围

这套脚本用于单机 Docker Compose 部署，服务器要保留原来的 `docker-compose.yml`、`.env`、数据库、Redis、日志和媒体目录，升级时只增加一个镜像覆盖文件，不会把仓库示例里的密码或路径写入线上配置。

生产升级前要先完成预发布联调，特别是 Sora、Lingjing、Mimo 视频任务、媒体缓存、额度扣除和失败退款，自动化测试通过不能替代真实渠道联调。

## 二、本次版本改动

一是同步官方源码并保留二开渠道、邀请记录、媒体清理、任务计费和管理页面，渠道编号继续兼容旧数据库；

二是修复后台设置保存失败后仍提示成功的问题，媒体清理间隔和保留时间会一起写入数据库，保存后刷新页面仍能显示新值，配置变化后定时任务会重新计时；

三是调整视频缓存，下载过程改为流式写入，支持总超时、大小限制、短间隔重试、临时文件清理和卡住任务恢复，数据库状态更新失败时会重试；

四是修复 Sora 平台 55 把 `Invalid token` JSON 当成视频的问题，缓存器会直接请求上游 `/v1/videos/{upstream_id}/content`，使用任务创建时选中的密钥，多密钥旧任务可以继续尝试其他启用密钥，JSON、HTML 和空响应不会再标记成功；

五是兼容 `wf-sd2-933` 文档协议，标准 Sora 的 `size`、`duration`、`input_reference` 和 `images` 会转换为 `aspect_ratio`、`resolution`、`seconds`、`image_url` 和 `reference_image_urls`，已经按文档传入的字段不会被覆盖；

六是修复 Lingjing、Mimo 上传渠道选择、任务落库失败补偿、邀请返利统计范围和部分前端类型问题，并增加了对应回归测试。

## 三、上线前准备

服务器至少要有 `docker`、`docker compose`、`curl`、`tar`、`gzip` 和 `sha256sum`，备份盘剩余空间要大于数据库、媒体、日志、数据目录和旧镜像大小之和，建议再预留一倍空间。

把 `deploy/production` 上传到服务器，例如 `/opt/new-api/deploy/production`，复制并修改配置：

```bash
cd /opt/new-api/deploy/production
cp release.env.example release.env
chmod 600 release.env
vi release.env
chmod +x ./*.sh
```

`release.env` 要按线上实际情况填写，重点核对 `COMPOSE_FILE`、服务名、容器名、数据库类型、数据库服务名、媒体目录、日志目录、数据目录和两个健康检查地址，不要使用示例密码。

源码必须来自已经审核并提交的 Git 版本，本地工作区有未提交修改时，`prepare-source.ps1` 会停止打包：

```powershell
./deploy/production/prepare-source.ps1 -ReleaseId 20260714-upstream-upgrade
```

上传压缩包和清单后，先核对 SHA256，再解压到 `SOURCE_DIR`：

```bash
sha256sum 20260714-upstream-upgrade-*.tar.gz
mkdir -p /opt/new-api/releases/20260714-upstream-upgrade/source
tar -xzf 20260714-upstream-upgrade-*.tar.gz \
  -C /opt/new-api/releases/20260714-upstream-upgrade/source
```

## 四、预检和备份

执行预检，脚本会记录 Docker 版本、Compose 配置、当前镜像、容器状态、磁盘空间和内外网健康检查：

```bash
cd /opt/new-api/deploy/production
./preflight.sh
```

安排短时间维护窗口，停止会产生重要写入的外部任务，再执行备份：

```bash
./backup.sh
```

默认配置 `STOP_APP_FOR_BACKUP=true` 会在备份数据库、媒体和数据目录时短暂停止应用容器，完成后自动启动旧版本并做健康检查，数据库和 Redis 容器不会停止，备份过程异常退出时也会尝试启动旧应用。

备份目录为 `BACKUP_ROOT/RELEASE_ID`，内容包含数据库备份、媒体目录、日志目录、数据目录、Compose、`.env`、反向代理配置、旧容器信息、旧镜像和 SHA256 校验文件，出现 `BACKUP_VERIFIED` 才表示本机校验完成。

本机校验完成后，还要把整个备份目录复制到另一块磁盘或对象存储，并抽查下面的内容：

```bash
cd /opt/new-api/backups/20260714-upstream-upgrade
sha256sum --check SHA256SUMS
test -s database/postgres.dump   # PostgreSQL
test -s database/mysql.sql       # MySQL
test -s database/one-api.db      # SQLite
tar -tzf files/media.tar.gz | head
gzip -t images/old-image.tar.gz
```

## 五、构建和部署

在服务器构建带固定版本号的镜像：

```bash
./build-image.sh
```

确认备份已经异地保存后部署：

```bash
./deploy.sh
```

脚本只重新创建应用容器，不重启 PostgreSQL 和 Redis，应用启动时会执行 GORM 自动迁移，因此数据库备份必须在部署前完成，部署失败时脚本会保存最近 300 行应用日志并停止继续操作。

## 六、上线验收

部署完成后要检查下面这些项目：

```bash
docker compose -f /opt/new-api/docker-compose.yml ps
docker inspect new-api --format '{{.Image}} {{.State.Status}} {{if .State.Health}}{{.State.Health.Status}}{{end}}'
curl -fsS http://127.0.0.1:3000/api/status
curl -fsS https://实际域名/api/status
LOG_SINCE=30m ./collect-logs.sh
```

管理端要检查登录、渠道列表、系统设置保存、邀请记录和任务详情，业务侧要实际完成一条普通模型请求、一条 Sora `wf-sd2-933` 视频、一条正在使用的二开视频渠道任务，并检查额度、任务状态、MP4 媒体地址和媒体目录文件。

至少观察两小时，重点查看错误率、接口耗时、数据库连接、Redis、磁盘空间、下载中的任务数量、下载失败原因、退款日志和额度变化，第一天内继续保留旧镜像和全部备份。

## 七、日志位置

每次脚本执行都会写入：

```text
BACKUP_ROOT/RELEASE_ID/run-logs/
```

问题诊断包位于：

```text
BACKUP_ROOT/RELEASE_ID/diagnostics/
```

诊断包包含 Compose 状态、经过筛选的容器状态、CPU 和内存、磁盘空间、健康检查结果和最近的应用日志，不采集容器环境变量，也不包含 `release.env`，文件权限为 `600`，反馈问题时同时提供任务 ID、请求时间、渠道 ID、模型、媒体状态和诊断包 SHA256；应用日志仍可能包含业务数据，对外发送前要做一次脱敏检查。

备份目录中的 `.env`、完整容器配置和数据库属于敏感数据，脚本使用 `umask 077` 创建文件，复制到异地存储时还要开启访问控制和加密，不能作为普通诊断材料直接发送。

## 八、回滚

只回退应用镜像，不恢复数据库：

```bash
CONFIRM_ROLLBACK=20260714-upstream-upgrade ./rollback.sh
```

数据库已经迁移且旧版本不能运行时，先停止外部写入，再恢复数据库：

```bash
CONFIRM_ROLLBACK=20260714-upstream-upgrade \
RESTORE_DATABASE=true \
CONFIRM_DATA_RESTORE=RESTORE-20260714-upstream-upgrade \
./rollback.sh
```

恢复数据库会丢失备份时间以后产生的数据，不能在业务仍有写入时直接执行，媒体目录也不会自动回滚，需要人工确认后再从 `files/media.tar.gz` 恢复，避免删除上线后新生成的视频。

## 九、停止条件

出现数据库备份校验失败、磁盘不足、旧镜像无法保存、健康检查失败、登录失败、计费异常、任务大量积压、Sora 再次生成 JSON 文件或错误率明显上升时，要停止继续操作，先执行 `collect-logs.sh`，再决定修复或回滚。

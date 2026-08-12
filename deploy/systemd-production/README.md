# New API systemd 生产发布

本目录维护一套“本地构建、Gitee Release 中转、服务器校验后切换”的发布链。大体积二进制不再经过本地到服务器的慢速 SSH 上传，同时保留现有的双 SHA-256、备份新鲜度、systemd 切换、内外网健康检查和自动回退。

> Gitee 只是程序制品的传输媒介，不是数据库备份。`deploy-fast.sh` 会同时检查全量备份和 binlog 备份；任何一项失败或过期都不允许发布，不应绕过这个门禁。

## 脚本职责

- `build-release.sh`：从已提交的 `HEAD` 生成 Linux amd64 程序、确定性 `main.gz`、`SHA256SUMS` 和 `RELEASE-MANIFEST.txt`。
- `publish-gitee-release.sh`：校验代码来源、双 SHA 和 gzip 内容，创建私有 Gitee Release，并上传三个附件。
- `deploy-fast.sh`：从匿名 HTTPS 或指定私有 Gitee 附件端点下载 `main.gz`，通过发布门禁后切换 systemd。
- `rollback.sh`：只回退程序和 systemd 配置，不自动回滚数据库。
- `deploy.sh`：保留的旧版“先 SSH/面板上传、再停服备份”流程；新的 Gitee 中转发布不调用它。

## 一次性配置

### 1. 准备 Gitee 权限

需要两个角色：

1. **发布令牌**：本地创建 Release 和上传附件，必须对 `qq1u/new-api` 有写权限。
2. **下载令牌**：服务器只下载私有 Release 附件。可以先使用同一令牌验证链路；长期建议用只加入该私有仓库的独立 Gitee 账号生成下载令牌。

在 Gitee 的“头像 → 设置 → 私人令牌”中新建令牌，开启项目/仓库（`projects`）权限。令牌只显示一次，不要写入仓库、命令参数或下载 URL。

先将本地发布令牌复制到 macOS 剪贴板，再执行：

```bash
install -d -m 700 "$HOME/.config/new-api"
pbpaste | tr -d '\r\n' > "$HOME/.config/new-api/gitee-release.token"
chmod 600 "$HOME/.config/new-api/gitee-release.token"
```

服务器下载配置只允许一条 `Authorization` 请求头。登录服务器后手工输入下载令牌，避免它进入 shell 历史：

```bash
install -d -m 700 /etc/new-api-deploy
read -rsp 'Gitee 下载令牌: ' GITEE_DOWNLOAD_TOKEN
printf '\n'
printf 'header = "Authorization: Bearer %s"\n' "$GITEE_DOWNLOAD_TOKEN" \
  > /etc/new-api-deploy/gitee-download.curl.conf
unset GITEE_DOWNLOAD_TOKEN
chmod 600 /etc/new-api-deploy/gitee-download.curl.conf
```

### 2. 安装服务器端脚本

脚本很小，仍可直接用 `scp` 上传。在本地仓库根目录执行：

```bash
scp deploy/systemd-production/deploy-fast.sh \
  deploy/systemd-production/rollback.sh \
  root@152.53.240.140:/tmp/

ssh root@152.53.240.140 \
  'install -d -m 700 /opt/new-api-deploy && \
   install -m 700 /tmp/deploy-fast.sh /opt/new-api-deploy/deploy-fast.sh && \
   install -m 700 /tmp/rollback.sh /opt/new-api-deploy/rollback.sh && \
   bash -n /opt/new-api-deploy/deploy-fast.sh /opt/new-api-deploy/rollback.sh'
```

不要在备份门禁异常时执行真实发布。可先检查两个备份任务的最近结果：

```bash
systemctl show new-api-cos-full-backup.service \
  -p Result -p ExecMainExitTimestamp --no-pager
systemctl show new-api-cos-binlog-backup.service \
  -p Result -p ExecMainExitTimestamp --no-pager
```

必须同时看到 `Result=success`，且全量备份不超过 25 小时、binlog 备份不超过 20 分钟。

## 每次发布

### 1. 同步并推送确定的代码

```bash
git status --short
git fetch origin upstream
git push origin HEAD:integrate/upstream-main-20260811
```

`build-release.sh` 默认要求工作树干净，而且始终只构建已提交的 `HEAD`。这可以防止未提交的本地文件意外进入生产包。

### 2. 构建制品

```bash
./deploy/systemd-production/build-release.sh
```

脚本会输出类似 `release-artifacts/<RELEASE_ID>` 的目录。检查内容：

```bash
ARTIFACT_DIR='release-artifacts/<RELEASE_ID>'
cat "$ARTIFACT_DIR/RELEASE-MANIFEST.txt"
cat "$ARTIFACT_DIR/SHA256SUMS"
gzip -t "$ARTIFACT_DIR/main.gz"
```

### 3. 先本地演练，再上传 Gitee

```bash
./deploy/systemd-production/publish-gitee-release.sh \
  --dry-run "$ARTIFACT_DIR"

GITEE_TOKEN_FILE="$HOME/.config/new-api/gitee-release.token" \
  ./deploy/systemd-production/publish-gitee-release.sh "$ARTIFACT_DIR" \
  | tee "$ARTIFACT_DIR/GITEE-RELEASE-OUTPUT.txt"
```

成功后会输出 Gitee 的数字 `release_id`、`main.gz` 的数字 `attachment_id`、固定下载 API URL，以及 `deploy-fast.sh` 的四个参数。`GITEE-RELEASE-OUTPUT.txt` 不含令牌，但仍属于本次发布记录，不要提交到 Git。

### 4. 发布前再检查备份

在服务器执行上文的两条 `systemctl show` 命令。任何一项不是最近的 `success` 时都应先修复备份，不执行下一步。

### 5. 执行切换

将上一步输出的四个值作为参数：

```bash
/opt/new-api-deploy/deploy-fast.sh \
  '<RELEASE_ID>' \
  '<MAIN_SHA256>' \
  '<MAIN_GZ_SHA256>' \
  'https://gitee.com/api/v5/repos/qq1u/new-api/releases/<RELEASE_NUMERIC_ID>/attach_files/<MAIN_GZ_ATTACHMENT_ID>/download'
```

脚本会依次执行：

1. 确认当前服务、内外网健康和两类备份正常。
2. 仅对 `qq1u/new-api` 的指定 Gitee 下载端点读取私有令牌。
3. 校验 `main.gz` SHA-256、gzip 结构和解压后 `main` SHA-256。
4. 备份现有配置和二进制，并创建新 release 目录。
5. 切换 systemd，验证内外网健康、真实运行二进制 SHA 和服务日志。
6. 任何切换后错误都恢复上一份 systemd 配置并重启原服务。

## 人工回退

自动回退只处理发布过程中的失败。如果上线后才发现业务问题，执行：

```bash
CONFIRM_ROLLBACK='<RELEASE_ID>' \
  /apps/releases/<RELEASE_ID>/rollback.sh '<RELEASE_ID>'
```

这条命令只恢复上一份 systemd 配置。数据库恢复会覆盖上线后的新数据，必须停止业务写入、人工核对备份并单独审批，本套脚本不会自动执行数据库恢复。

## 维护边界

- Gitee 私有 Release 附件不进入 Git 历史，不会放大仓库 clone 体积。
- 免费配额下的附件空间有上限，建议保留当前版、上一版和最近 8–10 个 Release；删除旧 Release 前先确认服务器仍有可用的本地回退包。
- 发布脚本不会复用已存在的 tag/Release。上传中途失败时，应在 Gitee 人工删除不完整 Release，再使用新的 `RELEASE_ID` 重新构建。
- 需要更换仓库时，必须同时审查发布端 API 与服务器端 URL 白名单；不要只把 owner/repo 改成环境变量。

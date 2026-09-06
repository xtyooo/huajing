# 视频生成接口说明

本文档说明画境网关的标准视频生成协议。推荐客户端使用 `POST /v1/videos` 创建任务，并通过 `GET /v1/videos/{task_id}` 查询结果。

## 鉴权

```http
Authorization: Bearer YOUR_API_KEY
Content-Type: application/json
```

## 创建任务

```http
POST /v1/videos
```

文生视频：

```json
{
  "model": "模型名",
  "prompt": "雨夜霓虹街道，镜头缓慢推进，电影感光影",
  "aspect_ratio": "16:9",
  "resolution": "720p",
  "seconds": "15"
}
```

单图、多图、参考视频和参考音频分别使用以下字段：

| 能力 | 单值字段 | 多值字段 |
| --- | --- | --- |
| 参考图片 | `image_url` | `reference_image_urls` |
| 参考视频 | `reference_video` | `reference_videos` |
| 参考音频 | `audio_url` | `audio_urls` |

组合示例：

```json
{
  "model": "模型名",
  "prompt": "保持人物身份和服装一致，参考素材生成电影感广告片",
  "image_url": "https://example.com/main-image.png",
  "reference_image_urls": [
    "https://example.com/ref-1.png",
    "https://example.com/ref-2.png"
  ],
  "reference_videos": [
    "https://example.com/reference-video.mp4"
  ],
  "audio_urls": [
    "https://example.com/reference-audio.mp3"
  ],
  "aspect_ratio": "16:9",
  "resolution": "720p",
  "seconds": "15"
}
```

首帧模式必须正好提供一张图片：

```json
{
  "model": "模型名",
  "prompt": "从这个画面开始，镜头缓慢推进",
  "image_url": "https://example.com/start-frame.png",
  "video_config": {
    "reference_mode": "start_frame"
  },
  "aspect_ratio": "16:9",
  "resolution": "720p",
  "seconds": "15"
}
```

首尾帧模式必须正好提供两张图片，且不能同时提供参考视频：

```json
{
  "model": "模型名",
  "prompt": "从第一张画面平滑过渡到第二张画面",
  "reference_image_urls": [
    "https://example.com/start-frame.png",
    "https://example.com/end-frame.png"
  ],
  "video_config": {
    "reference_mode": "start_end"
  },
  "aspect_ratio": "16:9",
  "resolution": "720p",
  "seconds": "15"
}
```

参考素材能力和数量限制由任务实际选中的模型及渠道决定，网关不会承诺一套虚假的统一上限。当前画境渠道的实际校验如下：

| 渠道 | 时长 / 分辨率 | 参考图片 | 参考视频 | 参考音频 | 其他限制 |
| --- | --- | --- | --- | --- | --- |
| naonao | 5–30 秒；固定 720p | 最多 10 张 | 不支持 | 最多 5 个 | `start_frame`、`start_end` 与 `frame` 均不支持 |
| manying | 4–15 秒；由 `sd-*` / `sdf-*` 模型固定 | 普通模式最多 9 张 | 普通模式最多 3 个 | 普通模式最多 3 个 | 首帧必须 1 张图，首尾帧必须 2 张图；帧模式不能带视频或音频 |

所有参考素材 URL 必须是公网可访问的 HTTPS 地址。若所选模型不支持某类素材或超过其限制，创建接口会在请求上游前返回结构化参数错误。

创建成功后返回公开任务 ID：

```json
{
  "id": "task_xxx",
  "task_id": "task_xxx",
  "object": "video",
  "model": "模型名",
  "status": "queued",
  "progress": 0,
  "created_at": 1776936222,
  "seconds": "15",
  "size": "1792x1024"
}
```

## 查询任务

```http
GET /v1/videos/{task_id}
```

标准状态为 `queued`、`in_progress`、`completed`、`failed`。任务生成成功后还需要将上游媒体缓存到画境；缓存期间保持 `in_progress` 和 99%，不会提前返回结果地址。缓存成功后响应包含：

```json
{
  "id": "task_xxx",
  "task_id": "task_xxx",
  "object": "video",
  "status": "completed",
  "progress": 100,
  "video_url": "https://example.com/media/task_xxx.mp4",
  "result_url": "https://example.com/media/task_xxx.mp4"
}
```

`video_url` 是文档标准字段，`result_url` 是同一稳定公开地址的客户端兼容别名。两者只来自缓存成功后的公开媒体地址，不会返回供应商的临时私有地址。

旧客户端仍可查询：

```http
GET /v1/video/generations/{task_id}
```

该接口保留既有响应和大写状态 `NOT_START`、`SUBMITTED`、`QUEUED`、`IN_PROGRESS`、`SUCCESS`、`FAILURE`、`UNKNOWN`。缓存成功后，`data.media_url`、`data.video_url` 和 `data.result_url` 三个字段值相同；已有字段不会被删除或改名。

## 下载视频

```http
GET /v1/videos/{task_id}/content
```

使用与创建、查询相同的 Bearer Token，并允许客户端跟随重定向。下载端点的鉴权、Range 请求和响应行为不因结果 URL 兼容字段而改变。

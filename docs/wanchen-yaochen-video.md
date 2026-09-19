# wanchen / yaochen 视频渠道

客户端沿用 `POST /v1/videos`、`GET /v1/videos/{task_id}`、
`GET /v1/videos/{task_id}/content`，Bearer 使用画境用户令牌。
上游 API Key 仅在后台渠道配置；向客户端返回画境公开任务 ID。

## 后台配置

| 渠道 | 编号 | 默认地址 | 模型 |
| --- | --- | --- | --- |
| wanchen | 78 | https://api.qilingze.com | SD2.5-满血-HN-720P |
| yaochen | 79 | https://api.chengyuapi.top | 用渠道密钥拉取 /v1/models，选择 video 对象中的实际模型 ID |

曜辰一个客户密钥绑定一个发布模型；不要把文档示例模型当成所有密钥的可用模型。
两者支持后台模型映射。使用公开模型名设置“按请求计费（固定价格）”；
适配器不添加时长或参考视频倍率，不在源码硬编码价格。
wanchen 本次只接 HN，未适配 CB/JL。

## 画境请求

wanchen 文生/多模态参考：

```json
{
  "model": "SD2.5-满血-HN-720P",
  "prompt": "让图中人物参考视频中的动作",
  "duration": 10,
  "aspect_ratio": "16:9",
  "resolution": "720p",
  "reference_image_urls": ["https://example.com/person.png"],
  "reference_videos": ["https://example.com/action.mp4"],
  "audio_urls": ["https://example.com/voice.mp3"]
}
```

HN 时长允许 5/10/20/30 秒，未提供时默认 5 秒；画幅未提供默认 16:9。
上游只收到字符串 seconds、aspect_ratio、images/videos/audios，
不会收到 duration/size/resolution/image_urls。
素材限 30 图/15 视频/15 音频，音频需同时有图片或视频，必须为公网 HTTPS。
参考视频与音频总时长最多 30 秒由上游核验，网关不会在提交时下载媒体测量。

yaochen 文生/多图参考（model 替换为管理员配置的公开名称）：

```json
{
  "model": "管理员配置的曜辰模型名",
  "prompt": "图一是人物，图二是场景，镜头缓慢推进",
  "duration": 30,
  "aspect_ratio": "9:16",
  "images": ["https://example.com/person.jpg", "https://example.com/scene.png"]
}
```

曜辰固定 30 秒，未提供时默认 30；显式其他时长报错。
允许 0–9 张 JPG/PNG 图片，支持完整 Data URI，保留图片顺序和重复项。
不支持视频/音频参考或首尾帧语义；提示词最多 12000 Unicode 字符。
内联图片执行解码/尺寸/合计 20MiB 检查；远程图片的实际格式、尺寸和全部
素材总大小由上游核验。URL 应为公网 HTTPS。

各已有列表别名同时接受一个 URL 字符串和 URL 数组。数组字符串
（例如把整个 JSON 数组再包在字符串内）不属于标准 JSON 素材列表。

## 轮询、缓存及失败处理

- 曜辰 queued/submitting/in_progress/unknown 继续查询原任务，不能因 unknown 重建任务。
- 上游完成后先缓存：缓存中进行中/99%，成功生成画境 URL 后成功/100%。
- 没有合法直链时使用渠道鉴权的 content 接口；307 跳转跨源时移除鉴权。
- 上游错误和缓存错误返回失败原因，私有任务 ID 和密钥不作为成功响应字段公开。
- 支持客户端 Idempotency-Key，缺省使用画境公开任务 ID。

本次使用模拟上游验证协议与回归，真实生成需管理员配置有效上游密钥后验证。

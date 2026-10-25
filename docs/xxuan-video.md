# xxuan 视频生成与视频超分

本次基于撤回官方升级后的 dev 接入，未发布、未调用真实付费生成。协议依据用户提供的 `/open/v1` 视频生成和视频超分文档。

## 后台配置

| 项目 | 视频生成 | 视频超分 |
| --- | --- | --- |
| 渠道 | xxuan 视频生成，类型85 | xxuan 视频超分，类型86 |
| 默认地址 | `https://api.xxuanaigc.cn/open/v1` | 同左 |
| 密钥 | 上游平台 API Key | 上游平台 API Key |
| 模型 | 自定义公开名，映射到 `/catalog` 返回的完整 `product_id` | 自定义公开名，例如 `xxuan-video-upscale` |
| 上游模型字段 | 映射后的值原样写入 `product_id` | 不发送 model/product_id |

生成渠道的“获取模型”读取 `/catalog`。超分接口的 `/video-upscale/options` 是规格表，没有商品 ID；管理端提供 `xxuan-video-upscale` 作为推荐公开别名。推荐列表不限制管理员添加其他名字。当前分辨率/帧率是否可用由上游校验，不按模型名推断能力。

为公开模型设置后台价格和分组倍率。使用既有按次/按秒配置；启用分辨率计费表时，按所选分辨率单价乘时长，且不再重复乘一次秒数。上游 `locked_price` 和目录价格以人民币分表示，但不会自动覆盖本地售价。超分的帧率不额外引入本地倍率。

后台渠道测试不会创建付费视频任务。余额查询未接入。模型目录读取也不能代替真实生成验证。

## 客户端请求

继续使用标准 `POST /v1/videos` 和 New API 用户令牌。以下地址和模型名均为占位示例，不会自动执行。

生成：

```json
{
  "model": "my-xxuan-video",
  "prompt": "以 @image1 为参考，镜头缓慢推进",
  "seconds": "10",
  "resolution": "720p",
  "ratio": "16:9",
  "images": ["https://media.example.com/reference.png"]
}
```

生成时长接受 `seconds`、`duration`、`duration_seconds`，内容必须为相同的正整数；不能用冲突别名覆盖计费。比例接受 `ratio`/`aspect_ratio`。不指定 resolution 时交由上游默认规格处理；如果后台启用了分辨率价格表，需要明确选定可计价的分辨率。

也可使用 `assets` 分组，保留数组顺序及上游 `@` 引用：

```json
{
  "images": ["ast_example", {"source_url": "https://media.example.com/a.png", "file_name": "a.png"}],
  "videos": ["https://media.example.com/reference.mp4"],
  "audios": ["https://media.example.com/reference.mp3"]
}
```

上面对象作为请求的 `assets` 字段；无素材时可以省略该字段或传 `{}`。asset ID 直接使用字符串，不包装成 `{asset_id: ...}`。素材 URL 必须是可直接读取的公网 HTTPS 原文件地址；带签名查询参数会原样保留。图片、视频、音频的标准单值/数组别名也会转换为对应分组。

超分：

```json
{
  "model": "xxuan-video-upscale",
  "video_url": "https://media.example.com/source.mp4",
  "resolution": "1080p",
  "frame_rate": 60
}
```

超分接受单个 `video_url`、`video`、`input_reference` 或只有一个元素的 `videos`；帧率可用 `frame_rate`/`fps`。不要求 prompt。网关在内部下载、上传并确认素材，实际建单正文仅有 `source.asset_id`、`resolution` 和 `frame_rate`。

源视频可信时长来自上游上传完成回执。例如 `duration_ms=6200` 按7秒预扣；客户端填写的 duration/seconds 不会改变超分计费时长。没有可信时长或余额不足时不创建付费超分任务。当前入口使用视频 URL，不接收二进制文件表单或已有 asset ID 作为超分源；上游未提供查询既有素材可信时长的接口。

源文件沿用网关 `MAX_MEDIA_DOWNLOAD_MB` 大小上限，并写临时文件，结束后清理。平台 API 请求使用渠道代理；源素材下载和存储上传使用现有 SSRF 保护的媒体请求路径及环境代理。媒体请求不携带平台 Bearer。

## 查询与重复提交

返回的是公开任务 ID，使用 `GET /v1/videos/{id}` 查询。上游任务 ID、建单时选中密钥和下载签名保存在私有任务数据中。`REVIEW_REQUIRED` 继续等待核对；上游成功但结果 URL 尚未就绪时保持处理中。视频进入既有本地缓存流程，缓存完成前最多99%，之后返回100%和稳定地址。

轮询按上游每次返回的 `next_poll_after_seconds` 至少等待相应时间，该时间保存在既有私有 JSON 字段中，重启后仍有效。没有新增数据库列或迁移。

生成接口明确不支持幂等去重。两个渠道在建单请求发出后遇到超时、连接中断或错误回执，网关都不会自动跨渠道再次建单；超分使用由本地公开任务 ID 派生的稳定幂等键。客户端重新发起一个新的 HTTP 创建请求仍可能生成新任务，这不构成端到端重复提交保护。不确定结果需结合上游调用记录核对，不能据本地报错断言上游没有创建或扣费。

## 编号与后续官方升级

既有58–81不变。82–84预留给暂停官方候选的 TaskPlugin/VLLM/SGLang；本次使用85/86，计数哨兵87。恢复官方升级时须保留这些不同身份，重新解决常量和注册冲突。当前源码不包含暂停候选的三个渠道实现。

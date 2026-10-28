# Silieco 宣传片 · V3 协作生态版

76 秒，1920×1080，30fps，亮色主题，H.264/AAC。表达 **人类员工与 Agent 数字员工，在 Silieco 中按照工作 SOP 共同协作**。SOP 让数字员工知道如何推进、执行边界在哪里，以及何时交给谁确认。

## 画面与角色

前 59 秒围绕 JAMOM「良品库盘点」：产品员工整理需求，跨机器数字员工按 SOP 与 Skill 执行，研发员工确认验收；全程保留原文、产物、讨论与责任人。

最后 17 秒从研发流程拉远，展现 Silieco 协作生态。四条 **可配置工作 SOP 示例** 为系统研发、内容生产、客户服务和数据分析。不同的人类员工与数字员工参与各流程，任务沿节点流转，Silieco 连接项目、任务、Skill 与上下文。

| 时间 | 内容 |
| --- | --- |
| 00–05 | 人类员工与数字员工，按工作 SOP 协同推进 |
| 05–14 | 产品需求与资料输入、任务交接 |
| 14–24 | 跨机器执行，获取关联任务、SOP 与 Skill |
| 24–34 | 如何推进、执行边界、何时交给谁、等待人类确认 |
| 34–42 | 到达人工确认节点，按 SOP 交接 |
| 42–53 | 人类员工与数字员工讨论并验证结果 |
| 53–59 | 项目沉淀协作上下文 |
| 59–65 | 从研发 SOP 拉远，展开多条工作流程 |
| 65–71 | 多位人类员工与数字员工按角色协作 |
| 71–76 | Silieco 协作生态与品牌收尾 |

头像均由用户提供，原文件复制到 `public/avatars/`。产品使用 `product.jpg`，研发使用 `developer.jpg`；生态全景使用 `human1–4.jpg` 与 `agent1–4.jpg`。通过渲染中的圆形/圆角裁切适配头像，未修改原始图片。人类员工与数字员工使用不同形状和文字标识。

人物姓名、机器名称、业务数据和生态中的 SOP 配置为演绎示例，不代表这些示例成员真实操作过项目或所有示例 SOP 均为内置模板。没有修改主应用及业务数据。

## 动效与音频

使用 Remotion 帧号控制曲线路径、错峰入场、头像反馈、负责人切换和生态镜头缩放。路径表达参考 [Anime.js](https://animejs.com/documentation/svg/createmotionpath/)，未使用会自行运行的 CSS 动画或计时器。

原创程序合成的 120 BPM 轻量背景音乐和动作提示音，无第三方录音采样，无人声。两条 WAV 均为 76 秒、48kHz 双声道；音效时间轴记录交接、接收、验收和生态展开的卡点。后期配音可单独调整音乐与音效。

## 无头导出

独立工程，Node 22+，`npm ci` 安装锁定版本。当前机器的 node_modules 为参考工程符号链接，迁移后安装即可。

```sh
npm run music
npm run render -- --browser-executable="/path/to/chrome-headless-shell"
python3 scripts/check-video.py out/v3/Silieco-人机协作宣传片.mp4 --expected-frames 2280
python3 scripts/delivery-v3.py
python3 scripts/finish-v3.py
```

全程使用 Chrome Headless Shell 149.0.7827.55，单并发、ANGLE、PNG 中间帧，并为 Remotion 浏览器子进程禁用 GPU 栅格化。不会打开 Studio 或可见浏览器。

本次前 59 秒复用已导出的 V3 画面，新增结尾按全局帧 1770–2279 无头导出，再以精确帧数拼接并混入 76 秒音频。完整 `npm run render` 可直接重建同一时间线，无需旧视频。

- `src/collaboration-film.tsx`：主片和人机协作场景。
- `src/ecosystem-ending.tsx`：四条 SOP、角色与生态镜头。
- `scripts/soundtrack-v3.mjs`：音乐和音效生成。
- `scripts/delivery-v3.py`：Caption JSON、SRT、配音时间轴与预览页。
- `out/v3/`：当前交付。
- V1、V2 源码和已交付视频仍保留。

## 本次验证

TypeScript 严格检查通过。成片音视频时长均为 76 秒，视频 2280 帧；完整音视频解码通过，未检出空白帧或孤立闪帧候选。已检查 14 个代表画面及 59 秒拼接位置。15 段配音字幕连续覆盖 00:00–01:16。

音乐与音效混音约 −28.1 LUFS，真峰值 −13.4 dBFS，无削波。自动闪帧检测属于启发式检查，已配合关键画面目视检查。

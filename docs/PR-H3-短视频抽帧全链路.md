# PR: feat: H3 短视频抽帧模式全链路（含 7900XTX 适配工作流）

> 目标分支：upstream/main
> 来源分支：feature/h3-short-video
> 变更规模：32 个文件，+3971 / -236

## 概述

本 PR 引入 MiniMax H3 短视频抽帧作为图片生成方式（`image_generation_mode = h3_video_frame`），并在 `h3_short` 轻量剧情模式下打通 H3 从场景图到多段视频渲染/拼接的完整链路，同时修复视频状态 reconcile 的时序 bug。

## 主要改动

- **图片生成新增 H3 抽帧模式**：H3 生成约 0.1s 短视频后抽帧（首/中/尾帧可选），支持附加提示词预设，抽帧链路复用 `minimax_h3_t2v/ref2v` 内置工作流。
- **H3 视频多段拼接**：超阈值镜头按用户设置的段长切段渲染后合并，修复模板默认 5s 兜底导致镜头时长不符、超阈值未切段、拼接段长硬编码等问题。
- **h3_short 轻剧情全链路接管**：去除 R2V 模式，短视频模式统一走 `h3_short`；新增 ref2v 场景图角色参考图注入（机制 A：`@图N` 标记 → `ref_image_1..8` 通道）。
- **视频状态修复**：`reconcileVideoOutputsFromDisk` 不再用单 segment 文件提前覆盖 `generating` 状态，避免拼接完成前前端误显示可播放。
- **LLM 引擎增强**：LM Studio 兼容开关、输出 token 配额可配置、流式空响应降级重试、输出配额字段 i18n。
- **其他**：全局种子 -1 归一化、负向提示词兜底修正、H3 抽帧设置项文案与提示优化。

## ⚠️ 兼容性警告（重要）

- 本次新增的 **3 个 H3 工作流**（`workflows/minimax_h3_t2v-gguf-api.json`、`workflows/minimax_h3_ref2v-gguf-api.json`、`workflows/minimax_h3_i2v-gguf-api.json`）及 **`Krea2_t2i_20260818_API.json`** 均为 **GGUF 量化适配版**：
  - 面向 **32GB RAM + AMD Radeon RX 7900 XTX（ROCm）** 环境调优，模型采用 Q4_K_M 量化（如 `Krea2_turbo_uncensored_edit_v1.1-Q4_K_M.gguf`、`qwen3-vl-4b-heretic-Q4_K_M.gguf`）。
  - 需要安装 **ComfyUI-GGUF** 插件；NVIDIA 或更大显存/内存环境可能需替换为未量化原件。
- **优化依据**：本 PR 的 H3 工作流优化参考自 https://github.com/happymy/MinimaxH3-7900xtx 。

## 涉及文件（节选）

- `internal/api/h3_video_frame.go`（+413）、`h3_video_frame_test.go`（+495）
- `internal/api/video_segments.go`、`videos.go`、`scenes.go`、`characters.go`、`settings.go`
- `internal/api/lightweight_story_generation.go` 及 `*_prompts_h3_short.go`
- `workflows/*.json`（4 个新增工作流）

## 测试

- 新增/更新单元测试：`h3_video_frame_test.go`、`lightweight_story_generation_test.go`、`lightweight_story_prompts_h3_short_test.go`。
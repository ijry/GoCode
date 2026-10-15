# YoYoCode UI 可用性缺口清单（roadmap）

> 记录时间：2026-10-14
> 背景：路由面已 520/523（P0 128/128），但**路由对齐 ≠ 产品可用**。本文档如实盘点
> mygo 原生 UI（`src/app/`，~5k 行）与「一个能日常用的编码 agent 桌面端」之间的差距，
> 按对「可用」的影响排序，供后续逐项补齐。上游前端参考：`_upstream/ref/src/components/`
> （settings 117 文件 / message 98 / layout 77 / chat 62 …）。

## 已修复（2026-10-14 续 + 2026-10-15）

- **P0-1 代码块语法高亮 + 复制**（提交 39eb948）。
- **P0-2 composer 模型/模式选择器**（提交 d9ae47c）：composer 加 Mode/Model pill 行,
  接 `session/set_mode` / `set_config_option`。
- **P0-3 工具卡 diff / 文件改动渲染**（提交 0a88105）：Edit/Write/Read/Bash 工具卡渲染
  结构化内容——编辑类着色统一 diff(红绿 + 行号)、读/写高亮代码、bash 渲染命令+输出,
  不再纯文本 dump。（2026-10-15）

- **后端错误裸 JSON 泄漏**：连不上 agent 时，聊天区直接显示
  `connect failed: acp_connect: {"code":"dependency_missing",...}`。现在 `apiError`
  解析后端 `{code,message,detail}`，渲染成红色横幅 + 可操作提示（如「Open Settings → Agents」）。
- **agent 起不来**：默认 agent `claude_code` 走 `npx` 拉包；本机 **node 无法派生子进程**（npx 挂死 150s+），
  故连接 60s 超时。修复：① `ResolveSpawn` 的 binary 分支在下载缓存缺失时**回退到 PATH 上已安装的二进制**
  （`opencode` 已装 → 直接可用，实测 connect 成功、冷启动 ~32s）；② 握手超时 60s→120s（`YOYOCODE_ACP_HANDSHAKE_TIMEOUT_MS`）。
- **agent 列表恒 available=false**：`acp_list_agents` 现在按「启动器是否可解析」打 `available`。
- **默认 agent 不智能**：`pickDefaultAgent` 优先选「可用且是直接二进制」的 agent（本机 → OpenCode），
  无则首个可用，再退首个。普通机器上等价于上游的「首个」。

## 现状（已具备）

- **聊天**：用户/助手气泡、thinking、工具卡（含 yoyocode-mcp 工作台卡）、composer（Send/Stop）、
  历史 turn 解析、权限请求卡（workspace `viewPendingCards` + pet 面板）。
- **Markdown**：✅ 已补（`src/app/markdown.go`）——围栏代码块、标题、有序/无序列表、引用、分隔线、
  行内 code/粗体/斜体/链接。
- **侧栏**：空间/最近/置顶分段切换（UI 优化点 1）。
- **底部状态栏**：connected + 版本 + MCP 健康 + 待办计数 + 更新提示（UI 优化点 2）。
- **设置**：16 个子页（general/appearance/web-service/quick-messages/agents/version-control/
  system/mcp/logs/skills/chat-channels/computer-use/experts/browser/science/office-tools）。
- **工作台**：automations / tasks（To-dos）/ forge（Repositories）/ canvas。
- **其它**：files / changes（diff）/ terminal / pet / login / import-sessions / project-boot。

## 缺口（按影响排序）

### P0 — 直接影响「能不能用」

1. **代码块高亮+复制**（已完成 2026-10-15，提交 39eb948，见「已修复」）。
2. **composer 模型/模式选择器**（已完成 2026-10-15，提交 d9ae47c，见「已修复」）。
3. **工具卡 diff / 文件改动**（已完成 2026-10-15，提交 0a88105，见「已修复」）。
4. **无附件/图片**。上游 composer 支持贴图/附件（`upload_attachment` 端口已有，UI 未接）。

### P1 — 明显影响体验

5. **无流式打字光标 / 自动滚动到底**。长回复时看不到「正在输出」。
6. **侧栏无搜索 / 重命名 / 删除的右键菜单**（若已有则降级）。
7. **工作台深度**：automations/tasks/forge/canvas 多为「列表 + 基本动作」，缺详情/编辑/空态引导。
8. **设置页深度**：16 页里若干只有开关/列表，缺表单校验、状态反馈、帮助文案。
9. **无 i18n**。上游有 12+ 语言包；现在全英文硬编码。

### P2 — 打磨

10. 键盘快捷键（⌘K 命令面板、⌘Enter 发送、Esc 停止）。
11. 空态/加载态/错误态的统一样式。
12. 通知/Toast 系统（操作反馈）。
13. 虚拟滚动（超长会话）。

## 建议的推进顺序

1. 附件/图片（P0-4）—— 接 `upload_attachment`。
2. 流式光标 + 自动滚动（P1-5）。
3. 其余 P1/P2 逐项。

> 注：本文档只列 UI（前端）缺口；后端路由/契约层的剩余项见 `docs/ROUTE_INVENTORY.md`。

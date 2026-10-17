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
- **P0-4 附件/图片**（提交 4f75c9d）：composer 接入 `upload_attachment`——paperclip
  打开宿主文件对话框、暂存托盘（缩略图/文件名/大小/✕）、发送前逐个上传并以 ACP blocks
  （text + image/file block）调 `acp_prompt`，用户气泡内联渲染附件。**P0 全部清空。**

- **P1-5 流式打字光标 + 自动滚动到底**（提交 c6454b8）：transcript 尾部 400ms 脉冲竖线
  （`c.After` 续帧，仅流式中跑），`TrackScroll` 跟随底部——**已在底部才跟随**，
  滚回历史的用户不被拽回（同上游 stick-to-bottom 的 at-bottom 守门）。
  打开会话即落在最新一条。（2026-10-15）
- **P1-7（部分）Automations 工作台深度**（提交 9a12ea4）：Automations 从「列表 + 刷新」
  升级为上游同构的两栏工作台——可选中列表 | 详情（状态 chip / 启用停用 / Run now·Edit·Delete /
  Schedule 事实 / PROMPT / RUN HISTORY），行右键菜单（Run now / Enable↔Disable / Edit / Delete
  带内联确认），创建/编辑表单（name/prompt/trigger/cron/timezone/agent/isolation/enabled，
  接 `automation_create`/`automation_update` 的 `{draft:{…}}` 契约），运行历史（状态/起止/时长/
  错误·摘要/Cancel），空态引导 + New automation。修复 cancel 未失效 runs 缓存导致运行行不刷新。
  （2026-10-15）
- **P1-7（部分）To-dos 工作台深度**（提交 2964bbb）：四列看板（To do / In progress /
  Needs attention / Done，列映射沿用上游 `columnForStatus`）+ 卡片（标题/状态 chip/agent·branch·
  N files +a -d）+ Show canceled / Show archived 开关；卡片右键菜单按状态给动作
  （Start/Edit、Cancel、Complete/Merge、Retry/Archive、Archive、Requeue/Archive、Unarchive，
  末尾 Delete 内联确认）；详情视图含事实 + ACTIVITY 事件时间线（`work_task_events`）；
  创建/编辑表单接 `work_task_create`/`work_task_update` 的 `{draft:{folderId,title,config}}` 契约；
  Start all；空态引导。（2026-10-16）
- **P1-7（部分）Canvas 工作台深度**（提交 480bcda）：板列表（颜色点 / 名称 / 描述 /
  N node(s) · M terminal(s) / edited …）+ 搜索框 + 行右键菜单 Open·Rename·Delete（行内确认）；
  新建/重命名表单（name/description/colour 12 主题色）接 `canvas_create_board`/`canvas_update_board`；
  打开板 → `canvas_list_nodes` 节点列表（kind/标题/坐标 + 删节点）+ ← Boards 返回；空态引导。
  （完整空间画布——平移/缩放/框选/拖拽——未做，节点列表是可检视的最小切片。）（2026-10-16）
- **P1-7（部分）Repositories(forge) 工作台深度**（提交 ac08d04）：从纯占位符升级为
  文件夹 forge remote 摘要（`folder_forge_remote`，免鉴权：provider + owner/repo + host/url）→
  无 remote / 不识别 host 的可操作提示 → 未连接时的「Connect a \<provider\> account」卡片
  （带「Open Version Control」跳设置）→ 已连接时的 identity + Issues/Pull requests 分页 +
  open/closed/all 过滤 + issue 列表（#号/标题/state/labels/author/comments）+ Close·Reopen
  + New issue 表单。顺带：`apiError.hint()` 增加 `config_missing`/`auth_failed`，
  `ensure` 失败时把 hint 存入 `errHints` 供面板渲染。（2026-10-16）
  **P1-7 四个面板（automations / tasks / canvas / forge）全部完成。**
- **P1-9 i18n 落地**（提交 760c664）：用上 mygo v0.5.5 的 i18n —— `locales/en.json` +
  `locales/zh-Hans.json`（ARB + ICU MessageFormat + CLDR 复数/数字/日期，78 条消息），
  `mygo generate` 产出带类型的 `locales/catalog_gen.go`（已提交，构建/测试不依赖 CLI）；
  `mygo.json` 开 `i18n` 配置。Settings → General 新增「语言」区（每种语言用 `i18n.Autonym`
  自称、当前项高亮、可跟随系统）。已迁移：状态栏、设置导航（21 页）、登录门、Servers 页、
  New chat / Send、侧栏会话数（复数）；`autoAbsTime` 改走 `i18n.FormatDate`（随窗口语言变化）。
  视图经 `cat(c)=locales.In(c.Locale())` 按窗口语言取消息 —— 真实应用里 mygo 让引擎语言与
  App 语言同步，且 headless Tester 可用 `tt.SetLocale` 驱动整窗验证。
  （坑：`mygo.App.Locale()` 首帧走主线程解析，无事件循环的测试里永久阻塞 → 视图只用 `c.Locale()`。）
  剩余面板字符串可按批继续迁入。（2026-10-16）

- **P1-6 侧栏搜索 + 会话右键菜单**（提交 d714778）：侧栏加会话搜索框（标题子串过滤、
  不区分大小写、✕ 清空、无命中提示 "No matches"）；每个会话行挂右键菜单
  `Rename / Pin↔Unpin / Delete`（Delete 走软删），与详情头部动作同源
  （`renameConversation` / `setPinned` / `deleteConversation`）。新增 `sidebar_test.go`
  （真后端栈驱动）覆盖搜索过滤/清空/无命中、菜单项与 pin 变体、重命名进内联编辑器、
  删除后行消失。（2026-10-15）
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

全部完成：代码块高亮+复制、composer 模型/模式选择器、工具卡 diff / 文件改动、
附件/图片（见「已修复」）。

### P1 — 明显影响体验

5. ~~**无流式打字光标 / 自动滚动到底**。长回复时看不到「正在输出」。~~（已完成 2026-10-15，提交 c6454b8，见「已修复」。）
6. ~~**侧栏无搜索 / 重命名 / 删除的右键菜单**。~~（已完成 2026-10-15，提交 d714778，见「已修复」。）
7. ~~**工作台深度**：automations/tasks/forge/canvas 多为「列表 + 基本动作」，缺详情/编辑/空态引导。~~
   （已完成 2026-10-15/16：automations 9a12ea4、tasks 2964bbb、canvas 480bcda、forge ac08d04。）
8. **设置页深度**：16 页里若干只有开关/列表，缺表单校验、状态反馈、帮助文案。
   （Quick Messages 全 CRUD + 校验/反馈/帮助文案已完成 2026-10-16，提交 ea51817；其余页待补。）
9. ~~**无 i18n**。上游有 12+ 语言包；现在全英文硬编码。~~（已完成 2026-10-16，提交 760c664，
   见「已修复」：zh-Hans catalog + 语言选择器 + CLDR 数字/日期；更多面板字符串可按批继续迁入。）

### P2 — 打磨

10. ~~**键盘快捷键**（⌘K 命令面板、⌘Enter 发送、Esc 停止）。~~
    （已完成 2026-10-16，提交 fd8ee8b：`ui.Modal` 命令面板 + 全局 `c.Shortcut` + composer `OnTextKey`。）
11. ~~**空态/加载态/错误态的统一样式**。~~
    （已完成 2026-10-16，提交 525673c：`ui_states.go` 的 `loadingState`/`emptyState`/`errorState`，
    errorState 带后端 hint + Retry。）
12. ~~**通知/Toast 系统**（操作反馈）。~~（已完成，提交 a40b316：`c.AddToast` + `ToastViewportBase`。）
13. ~~**虚拟滚动**（超长会话）。~~
    （已完成 2026-10-16，提交 8bb372f：transcript 改用 mygo `ui.List` + `ListState.FollowEnd`，
    只 build 视口内的行。**P2 全部清空。**）

## 建议的推进顺序

1. ~~设置页深度续（P1-8）。~~（已完成：Version Control b0992e4；Quick Messages ea51817。）
2. ~~其余 P2 逐项（快捷键、空态/加载态、Toast、虚拟滚动）。~~（**已全部完成**，2026-10-16。）
3. i18n 续：把各工作台面板的字符串按批迁入 locales/（外壳已中文化）。

> 2026-10-16 状态：**P0 / P1 / P2 全部清空**。Documents（NSDocument 式）也已落地（6fa3f54 + 0a01e1e）。
> 剩余为「i18n 面板字符串批量迁入」这类持续性打磨，不再是阻塞「可用」的缺口。

## 宏观结构对齐（2026-10-17 起）

> 用户指令：「细节可以尽量使用原生控件，但是**宏观上 UI 功能都必须参照上游复刻**」。
> 即：上面 P0–P2 的「可用性」缺口清空后，转入**按上游的信息架构补齐宏观结构**——
> 上游有的主面板 / 路由 / 工作台，我们不能缺。

逐步对照 `_upstream/ref/src/app/` + `src/components/` 逐项补齐，按结构影响排序：

- [x] **AuxPanel 右栏**（提交 dce25e3）：工作台右侧固定栏，Session / Files / Changes /
  Commits 四个 tab；选中项在主区**整体替换**显示明细（原并排布局会把明细挤到 ~300px
  导致按钮点不到）。上游 `components/layout/aux-panel.tsx`。
- [x] **会话标签条 Conversation Tabs**（本次）：工作台主区顶部的浏览器式标签条——
  每个打开的会话一个 tab（状态点 / 标题 / 关闭按钮），点击切换、右键
  `Close / Close Others / Close All`、尾部「新建会话」按钮 + Terminal 开关；
  经 `list_opened_tabs` / `save_opened_tabs` **按 CAS 版本持久化**（切 tab 写回
  `is_active`，重启/多窗口一致）。上游 `components/tabs/tab-bar.tsx` + `tab-item.tsx`
  + `stores/tab-store.ts`。
  **未做（有意）**：上游的 IDEA 式分屏组（`groupLayout` / 跨组拖拽 / tile 模式）与
  设备本地 draft tab——两者都是独立大子系统，且非「条本身可用」所必需；本 build 的
  「新建会话」已直接 `create_chat_conversation`，故无需 draft 概念。
- [ ] **Git 操作面**：上游是 commit / merge / stash / push 四个**独立窗口**
  （`src/app/commit|merge|stash|push`）。本 build 做**整页路由**（`workbench`），
  后端 `git_log` / `git_branch` / `git_commit` / `git_diff` / `git_merge` / `git_push` /
  `git_stash` 已就绪。
- [ ] **分支选择器**（composer 上的 branch picker，上游 `conversation-branch-tag`）。
- [ ] **Terminal 移到底部面板**：上游 terminal 是「底部可折叠面板 + 多 terminal tab」，
  现在是主区的一个 tab——需改建为底栏。
- [ ] **project-boot / import-sessions 实体化**：现在可从状态栏进入，面板仍是薄壳。
- [ ] **命令运行器 command runner**、**clone / remote-manage / workspace-folder / upload 对话框**。
- [ ] **token-usage 工作台**、**browser 面板**、**settings → model-providers 页**。

> 注：本文档只列 UI（前端）缺口；后端路由/契约层的剩余项见 `docs/ROUTE_INVENTORY.md`。

# GoCode UI 优化点（与上游 codeg 的差异与改进）

> 记录时间：2026-10-13
> 背景：GoCode 是把上游 codeg（Tauri/Rust/Next.js）复刻到 Go + mygo 原生 UI 的端口。
> 整体 IA 已对齐上游（左侧栏即会话侧栏，无多余外层导航栏），但以下 3 处在「大体一致」
> 的基础上需要做细节优化。本文档单独记录这 3 点，供后续实现时对照。

---

## 1. 侧栏「空间 / 最近 / 置顶」改成切换形式，不要垂直堆叠

> **状态：✅ 已落地**（`src/app/workspace.go` 的 `sidebarSegmented` + `viewWorkspace`）。
> 侧栏中间区改为顶部分段切换 `Recent | Pinned | Folders`，同一时刻只渲染选中项的一个滚动列表，
> 彻底消除三块垂直堆叠的长滚动。文件夹从底部固定区移入「Folders」tab；会话的新建/agent 选择器
> 在 Recent/Pinned 下显示，Folders 下显示「Open folder…/⟳」。`sidebarTab` 默认 `recent`。
> 测试：`shell_test.go` 的 `TestSidebarSegmentedSwitcher`（切换只显当前项）。

### 现状（上游 codeg 的痛点）
上游侧栏把三个分组**垂直堆叠**在同一列里：

- **空间（Folders）** —— 已打开的工作文件夹列表
- **最近（Recent）** —— 当前文件夹下的会话（按时间）
- **置顶（Pinned）** —— 置顶会话（永远在 Recent 之上）

代码位置：`src/components/layout/sidebar.tsx`
- 分组顺序由 `DEFAULT_SECTION_ORDER`（`SidebarSectionOrder`）控制；
- 仅 `Recent` 有一个可见性开关 `showRecent`（`RECENT_HIDDEN` / `NO_HIDDEN_SECTIONS`）；
- 三个分组全部渲染后，长会话列表 + 长文件夹列表会让侧栏滚动很长，体感差。

我们当前端口（`src/app/workspace.go`）复刻了上游的堆叠：
`nav 块 → 会话（Pinned → Recent）→ Folders（置最后，MaxHeight 180 自限高）`。
同样存在「三块都展开时滚动过长」的问题。

### 优化方案
把这三类做成**顶部分段切换（segmented control / switcher）**，同一时刻只展示
被选中的那一块列表，从根本上消除垂直堆叠导致的超长滚动。

- 切换器固定在侧栏 `nav 块` 之下、列表之上；
- 三个分段：`[空间] [最近] [置顶]`（图标 + 文字，选中态高亮，参考上游 `SidebarNavButton` 的 `bg-sidebar-primary/8` 选中样式）；
- 下方只渲染当前选中的那一个列表（单区域可滚动），其余两块不渲染；
- 会话数/文件夹数可作为分段上的计数徽标（badge），沿用上游 `trailing` 计数思路；
- 默认选中「最近」（与上游 `showRecent` 默认 ON 一致）；
- 分段选择状态进 `Shell` 内存字段（如 `s.sidebarTab`），持久化可选（对齐上游 localStorage 习惯）。

### 样式建议
- 分段控件用一行等高胶囊/下划线式 tab，左对齐，与上方 nav 图标列同一条视觉轴；
- 选中段：浅填充 + 主色文字；未选中：muted 文字，hover 微填充；
- 每个分段下方接一个 `ui.Scroll`（Grow(1)），列表项沿用现有 `sideRow` 几何。

---

## 2. 底部状态栏：保留 connected + 版本号，补上 codeg 的其它状态栏功能

> **状态：✅ 已落地（可测子集）**（`src/app/app.go` 的 `viewStatusBar` + `viewStatusMcp/Tasks/Update`）。
> 保留左下 `connected/connecting` + URL、右下 `GoCode v0.1.0`；在右侧区块切换与版本号之间补：
> - **MCP 健康**（`viewStatusMcp`）：读 `get_codeg_mcp_service_status` → `running` 绿点 / 否则灰点 + `MCP`。
>   （companion 未内置 → `stopped`，属诚实状态；见点 3）
> - **待办计数**（`viewStatusTasks`）：读 `work_task_list` → 未 done 的数量，`☑ N` 徽标，点击跳到 To-dos；为 0 时隐藏。
> - **更新提示**（`viewStatusUpdate`）：读 `check_app_update` → 有新版才显示 `↑ Update`（本构建不自更新 → 恒隐藏，前向兼容）。
> 测试：`shell_test.go::TestStatusBarIndicators`。
>
> **暂缓（需端口暂不具备的信号，已在代码注释）**：
> - **告警铃** `StatusBarAlerts`：需「未读失败/关注」信号，port 的 `automation_list` 不含 unseen/attention 字段 → 暂不做。
> - **计算机使用指示** `StatusBarComputer`：port 的 `computer_*` 是 refused 桩，恒不可用 → 恒隐藏（上游未开启时同样不渲染）。
> - **命令启动器** `CommandDropdown`、**quick-actions** `QuickActionsDropdown`、**StatusBarStats**：交互式菜单/客户端 task-context 统计，
>   非纯状态展示，端口无对应 store → 暂缓。

### 现状
我们端口 `viewStatusBar`（`src/app/app.go`）目前只有：
- 左下：`● connected`（绿点）/ `connecting` + 后端地址 URL
- 右下：`GoCode v0.1.0` 版本号

上游 codeg 状态栏（`src/components/layout/status-bar.tsx`）功能远多于这两样，
应一并补进我们的底部 chrome。

### 上游状态栏组成（需移植的功能）
左侧（窗口左下角，对齐侧栏图标轴）：
| 组件 | 作用 |
| --- | --- |
| `QuickActionsDropdown` | 快捷操作启动器（左下角入口） |
| `StatusBarStats` | 工作区统计（如用量/活动模型等） |

右侧（从左到右）：
| 组件 | 作用 |
| --- | --- |
| `StatusBarUpdate` | 更新可用提示 |
| `StatusBarTasks` | 待办任务计数（`attentionCount`） |
| `CommandDropdown` | 命令启动器 / 分支选择 |
| `StatusBarComputer` | 计算机使用（agent 当前可看窗口）指示，未开启时不渲染 |
| `StatusBarMcp` | **codeg-mcp 服务健康**（见第 3 点），与告警铃相邻 |
| `StatusBarAlerts` | 告警铃（未读失败/注意数） |

移动端（`isMobile`）为精简版：左 `QuickActionsDropdown` + `StatusBarStats`；
右 `StatusBarUpdate` + `CommandDropdown` + `StatusBarMcp` + `StatusBarAlerts`。

### 优化方案
在我们的 `viewStatusBar` 右侧依次补上对应控件（用 mygo 原生组件实现）：
1. **更新提示** `StatusBarUpdate` → 有更新时显示一个升级徽标/按钮；
2. **待办计数** `StatusBarTasks` → 从 `work_task_list` 取进行中/需关注数量，铃或数字徽标；
3. **命令启动器** `CommandDropdown` → 底部 chrome 的一个命令入口按钮；
4. **计算机使用指示** `StatusBarComputer` → 仅在桌面运行时开启计算机使用时渲染；
5. **gocode-mcp 健康** `StatusBarMcp` → 显示 companion MCP 服务是否在线（第 3 点配套）；
6. **告警铃** `StatusBarAlerts` → 未读失败/注意计数（取 `unseenFailures` / `attentionCount`）。

保留现有 `connected`（左下）+ 版本号（右下）不动。各控件用 `ui.IconButton` +
`ui.Text` + 计数徽标实现，间距沿用现有 `Gap(8)`。

---

## 3. 对应一个 `gocode-mcp`（上游为 `codeg-mcp`）

> **状态：✅ 已落地**（`src-go/mcp/companion.go` + `cmd/gocode-mcp` + `src/app/codegmcp.go` + `src-go/acp/companion.go`）。
> 应用侧接线已完成：`acp.Manager` 持有 loopback 后端地址 + bearer token（`SetCompanionBackend`，
> 启动 `MarkRunning` 后注入），`ConnectLive` 在 `session/new` 的 `mcpServers` 里追加
> `gocode-mcp` 的 stdio MCP server 条目（env 注入 `GOCODE_API`/`TOKEN`/`AGENT_TYPE`/`SESSION_ID`），
> agent runtime 据此每会话拉起 companion。二进制解析顺序 `GOCODE_MCP_PATH` → 可执行文件同级 → PATH
> （端口 `locate_codeg_mcp_binary`），缺失则**跳过注入**（不注册幽灵路径，严格 agent 不会因此拒掉整会话）。
> 测试：`src-go/acp/companion_test.go`（5）+ `src-go/acp` 全量 + `acp_live` 集成回归绿。
> 打包（见落地点回顾 #3）：本端口是 Go 单二进制桌面应用，无 Tauri sidecar 概念；
> `gocode-mcp` 与 `gocode` 同目录（或置于 PATH）即被解析，发布流程 `go build ./cmd/gocode-mcp` 一并产出即可。
> 一个真实可跑的 MCP stdio server（JSON-RPC 2.0）：`initialize` / `tools/list` / `tools/call`；
> 暴露同一套 6 个 workbench 工具，名字归一化 `mcp__gocode-mcp__X / gocode-mcp/X → X`。
> `create_automation`/`create_work_task` **代理到真后端**（loopback `/api`，env: `GOCODE_API`/`TOKEN`/
> `FOLDER_ID`/`AGENT_TYPE`/`SESSION_ID`）；`get_session_info`/`task_progress`/`task_complete` 回 ack；
> `resume_delegation` 诚实报 not-supported（委派引擎未移植）。
> **会话内卡片**：`src/app/codegmcp.go`（codeg-mcp-tool.ts 端口）——识别这 6 个工具，走包装键
> `arguments/input/params/payload/_meta/args` 的 peel（含双重 JSON），提取单个关键参数 + verdict/prompt/reason，
> 剥掉 content[0].text / structuredContent.note / host error / Codex `Wall time…Output:` 包装的结果文本，
> 折叠行显示 `<tool> · <detail>`，展开显示结果/verdict/prompt/reason + running/ok/err。
> 测试：`src-go/mcp/companion_test.go`（7）+ `src/app/codegmcp_test.go`（5）全绿。
>
> **应用侧接线（已完成）**：`src-go/acp/companion.go`（`ResolveCompanionBinary` /
> `CompanionMCPServer`）+ `acp.Manager.SetCompanionBackend` / `companionSpec` +
> `connection.go` 的 `ConnectLive` 把 companion 注入 `session/new` 的 `mcpServers`。
> 启动期（`cmd/gocode/main.go` 与 `cmd/gocode-server/main.go` 的 `MarkRunning` 之后）调用
> `SetCompanionBackend(url, token)` 把 loopback 后端地址 + bearer token 交给引擎；
> `ConnectLive` 据此构建 stdio MCP server 条目（env 注入 `GOCODE_API`/`TOKEN`/`AGENT_TYPE`/
> `SESSION_ID`），agent runtime 每会话拉起 companion。二进制缺失时优雅跳过（不注册幽灵路径）。
> 注：`GOCODE_FOLDER_ID` 当前留空（companion 的 `create_work_task` 会省略 folder 作用域）；
> 后续可从 `conversationID` 经 db 解析 folder id 再注入，属增强非必需。

### 上游是什么
`codeg-mcp` 是随 codeg 分发的**独立 companion MCP server 二进制**（不是前端模块）：

- 二进制入口：`src-tauri/src/bin/codeg_mcp.rs`（Rust）
- 工具 schema：`src-tauri/src/acp/delegation/tool_schema.json`
- 提供的「workbench 伴侣」工具（`src/lib/codeg-mcp-tool.ts` 的 `CODEG_MCP_WORKBENCH_TOOLS`）：
  - `get_session_info`
  - `task_progress`
  - `task_complete`（verdict: `success` / `needs_review` / `blocked`）
  - `create_automation`
  - `create_work_task`
  - `resume_delegation`
- 前端渲染：这些工具调用经 `CodegMcpToolCard`（`src/lib/codeg-mcp-tool.ts` 解析）
  在会话流里渲染成一行摘要（而不再是裸参数 dump）；
- 工具名归一化：`mcp__codeg-mcp__task_progress`、`codeg-mcp/task_progress` 等
  都折叠到 canonical 名（见 `tool-call-normalization.ts`）；
- 状态栏健康：`StatusBarMcp` 显示该 companion 服务是否在线。

### 我们需要的 `gocode-mcp`
按复刻 + 改名规则（`codeg-mcp → gocode-mcp`，请求 id `codeg-models → gocode-models`
等已在其它路由落实），需要：

1. **Go 版 companion 二进制**：在 `src-go/...`（建议 `cmd/gocode-mcp` 或
   `src-go/mcp/`）实现与上游等价的 MCP server，暴露同样的 6 个 workbench 工具，
   经同一套 JSON-RPC/MCP 线协议与宿主通信（argument 包装键 `arguments`/`input`/
   `params`/`payload`/`args` 的 peel 逻辑需对齐 `codeg-mcp-tool.ts` 的 `findArgs`）。
2. **工具 schema**：把上游 `tool_schema.json` 的 6 个工具定义落到 Go 侧
   （`go:embed` 或代码常量），保证参数/verdict 校验一致。
3. **原生 UI 渲染**：在 mygo 侧实现等价于 `CodegMcpToolCard` 的卡片——
   折叠行只陈述每个工具的关键参数（`get_session_info`→session id、
   `task_progress`→message、`task_complete`→summary+verdict、
   `create_automation`/`create_work_task`→name/title 且额外审计 `prompt`、
   `resume_delegation`→task_id + `reason`），并展示 peeled 后的结果文本与
   `running`/`ok`/`err` 状态。
4. **状态栏健康指示**：`StatusBarMcp` 改为反映 `gocode-mcp` 在线状态（第 2 点配套）。
5. **绑定/打包**：上游用 `prepare-sidecars.mjs` 把 `codeg-mcp` 作为 sidecar 打包进
   Tauri；Go 端口需在构建/发布流程里把 `gocode-mcp` 二进制一并打进安装包。

### 改名清单（本点涉及）
- 二进制/服务名：`codeg-mcp` → `gocode-mcp`
- 工具命名空间前缀：`mcp__codeg-mcp__*` → `mcp__gocode-mcp__*`
- 相关内部标识沿用既有 rebrand 规则（vendor 名 `codeg` → `gocode`）

---

## 落地点回顾
| # | 文件 | 现状 | 目标 |
| --- | --- | --- | --- |
| 1 | `src/app/workspace.go` | 侧栏三块垂直堆叠 | 顶部分段切换，单块可见 |
| 2 | `src/app/app.go`（`viewStatusBar`） | 仅 connected + 版本号 | 补更新/待办/命令/计算机/mcp 健康/告警 |
| 3 | 新增 `cmd/gocode-mcp` + 原生卡片 + 应用侧注入 | 无 | 等价 codeg-mcp 的 Go companion |

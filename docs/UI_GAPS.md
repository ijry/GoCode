# GoCode UI 可用性缺口清单（roadmap）

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
  **空间画布（本次）**：节点按存储的 x/y **绝对定位**在 2400×1600 的 world 上，world 带
  `TransformOrigin(0,0)` + `Translate`/`Scale` 承载平移缩放；**拖卡片移动**（`Dragged` 累积 →
  `DragEnded` 提交 `canvas_move_nodes`，坐标精确落库）、**拖空白处平移**、**头部 − / % / + /
  Reset view** 缩放（0.4–2.0，默认 100%）、**New note**（`canvas_create_node`，按视口左上 +
  错位偏移落点）、每卡 × 删节点、kind 徽标本地化（note/group/agent/conversation/folder/
  file/terminal/custom）。坑：world 必须 `Absolute()`，否则 2400px 会把父容器撑开、把头部按钮
  挤出窗口。**多选（本次）**：背景拖拽在 **Select / Pan 两段式模式**下分别做框选与平移
  （分段控件高亮当前模式）；框选（`canvasBand` 世界坐标 + 半透明带）松开后按矩形相交选中卡片、
  选中卡加 accent 边框；拖任一选中卡 **整组移动**（一次 `canvas_move_nodes` 传多条目）、
  点卡片单选/再点取消；头部「N selected + Delete selected」走 `canvas_delete_nodes` 批量删。
  坑：拖拽释放帧 `Clicked()` 也会为真，会把刚移动的多选塌缩成单选 —— 必须**先处理拖拽**
  并用 `dragged` 标记跳过该帧的点击选择。**节点尺寸拖拽已完成**（2026-11-02）：选中卡片右下角 20×20 resize 手柄
  拖拽，增量落库走 `canvas_update_node` 的 width/height patch，后端 clamp 48–20000；
  手柄仅在选中态出现，tooltip「调整大小」。测试 `TestCanvasNodeResizeDrag`
  （拖拽 +80/+60 → 落库 280×180）。**滚轮/捏合缩放 + Delete 键 + region 分组已完成**（2026-11-03）：
  - **滚轮/捏合**：world 元素 `Focusable().HandleInput(...)`（mygo 0.7）——`InputScroll` 平移
    （DX/DY 已在 world 轴上，pan -= zoom·delta，与滚动容器同向）；`InputZoom`（捏合 / Windows
    触摸板 Ctrl 滚动）与 Ctrl+滚轮 notch（×1.25 / ×0.8）经 `zoomCanvasAt` 以指针为锚缩放
    （pan += w·(old−new)，指针下的世界点不动）。fn 拒绝指针事件，卡片点击/拖拽不受影响。
  - **Delete/Backspace**：world 可聚焦（点画布任意处即获得键盘），`InputKeyDown` 里
    Delete/Backspace（有选中时）置 `canvasKeyDelete` 标志，下一帧 View 消费并走
    `canvas_delete_nodes`；无选中不吞键。
  - **region 分组**：选中含会话卡片时头部出现「Group into region」——`canvasRegionPlanOf`
    照上游 `groupSelection` 收集成员（`conversation_id`）与待消费的松散卡、按选中框
    （+12 padding，整卡列宽 224×132·gap12，≤6 列）算 region 框几何，`canvas_group_into_region`
    成功后消费卡删除、region 成为选中；region 卡显示「N members」。上游在框内网格渲染成员卡，
    本 build 只显示计数（诚实最小切片）；`canvas_detach_member` 未接 UI。
  测试：`TestCanvasNodeDragAndZoom`（拖拽落库 + 缩放百分比）、
  `TestCanvasPanAndNewNote`（平移位移 + 新建便签）、
  `TestCanvasMarqueeSelectMoveAndDelete`（框选 2/3 → 组拖两条落库 → 批量删）、
  `TestCanvasNodeResizeDrag`（选中后拖右下角手柄改尺寸落库）、
  `TestCanvasWheelPanAndPinchZoom`（触摸板平移精确位移 + 捏合锚点随指针移动）、
  `TestCanvasDeleteKeySelection`（点选卡片 → Delete 键 → 节点删除、邻卡保留）、
  `TestCanvasGroupIntoRegion`（框选两会话卡 → 按钮出现 → region 框几何 (28,28 484×196)、
  member_ids 保序、消费卡删除、region 选中 + 成员计数），
  `canvas-board-spatial.png` / `canvas-board-selection.png` / `canvas-zoom-pinch.png` /
  `canvas-region.png`。（2026-10-19，resize 2026-11-02，wheel/delete/region 2026-11-03）
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
  （`opencode` 已装 → 直接可用，实测 connect 成功、冷启动 ~32s）；② 握手超时 60s→120s（`GOCODE_ACP_HANDSHAKE_TIMEOUT_MS`）。
- **agent 列表恒 available=false**：`acp_list_agents` 现在按「启动器是否可解析」打 `available`。
- **默认 agent 不智能**：`pickDefaultAgent` 优先选「可用且是直接二进制」的 agent（本机 → OpenCode），
  无则首个可用，再退首个。普通机器上等价于上游的「首个」。

## 深化（2026-10-18）

- **会话详情 i18n 收尾**：会话头（重命名输入框的 保存/取消、重命名/置顶/取消置顶/删除）、
  composer（占位文案、Attach、Stop、Send 已有；`connecting/prompting/error/disconnected`
  四态的 hint 与 warning strip）、工具卡（`Tool` 回退、`Completed/Failed/Rejected/Running/
  Queued/Pending` 状态徽标、`edit N` 序号、`No output recorded.`）、错误 note、
  permission 卡（标题/提问/回答提示），以及**会话工作台全部 actionMsg toasts**（cancel/create/
  rename/pin/delete/open/new-chat/prompt/upload 的成功与失败文案，失败带 `{message}` 错误详情）
  共 ~55 条新键迁入 `locales/`（en + zh-Hans）。goroutine 文案按定式捕获 `loc := c.Locale()`；
  `toolStatusLabel`/`composerHint`/`composerStrip`/`buildTranscript` 加 `locales.Messages` 首参，
  发请求的方法加 `loc string` 首参（调用链含 palette Esc、tab 条、pet 面板）。
  新增 `TestConversationLocalizes`（zh 断言 重命名/置顶/删除/已完成/agent 正在处理…/停止/发送）+
  `conversation-zh.png`。

- **设置各页正文 i18n**：把通用（跟随系统）、Web 服务、智能体、系统、MCP、日志、技能
  （含自定义技能区）、聊天频道、计算机机使用、专家、外观、浏览器（tools + bridge 状态）、
  办公工具、科学、快捷消息（列表 + 编辑器 + 校验 + 保存/删除 toast）各页正文迁完，
  共 ~95 条 `set*` 新键（页头复用已有 `nav*`；计数/版本/错误详情走 `{count}`/`{scope}`/
  `{version}`/`{message}`/`{name}`/`{page}` 占位）。新增 `notifyErr()` 让本地化失败 toast
  仍以 error 样式呈现；toast 的关闭按钮标签本地化。新增 `TestSettingsPagesLocalize`
  （zh 七页 chrome 断言）+ `settings-zh-browser.png`。

- **版本控制设置页 + 工作台面板 i18n**：版本控制页（git 卡 / forge 账号卡 / 添加表单 /
  token 轮换 / test-save-remove toasts，~40 条 `vc*` 键）；工作台四面板 —— Automations
  （列表 / 详情 / 编辑器 / 右键菜单 / 六态状态徽标 / toasts，~45 条 `au*`）、Canvas
  （~25 条 `cv*`）、Repositories（连接态 / 无远程 / 不支持主机 / issue 行 / 表单，~35 条 `fg*`）、
  Pet（会话行 / 空态 / 权限卡，7 条 `pp*`）。失败 toast 统一走 `notifyErr()`。
  新增 `workbench_i18n_test.go::TestWorkbenchLocalize`（zh 四面板断言）+
  `workbench-zh-automations.png`。**顺手修**：`TestAutomationDetailAndRunHistory` 首帧
  断言 "No runs yet." 的异步竞态改为 `waitText`。至此设置页与工作台面板英文基本清零，
  剩余只有数据值（agent/scope/trigger/isolation 枚举、UTC、GitHub 等品牌名）。

- **待办看板 i18n**（To-dos）：~70 条 `tk*` 键 —— 四列表头（`taskColumns` 改为函数，
  按语言取标签）、空态、卡片 meta、右键菜单（Start/Edit/Cancel/Complete/Merge/Retry/
  Archive/Requeue/Unarchive/Delete 按状态出现）、详情事实行（Agent/Branch/Changes/
  Verdict/Summary/Error）、活动时间线、编辑器、九态状态徽标与全部 mutation toast。
  失败 toast 走 `notifyErr`；顺手去掉 `taskTransition` 失败 toast 里的路由名泄露。
  `TestWorkbenchLocalize` 扩到 to-dos 列头断言 + `workbench-zh-todos.png`。

- **死键审计 + 导入面板补漏**：全 catalog 扫了一遍「accessor 定义了但 UI 没引用」的键，
  6 个真死键处理掉——3 个带参确认提示接进确认行（`gitRemoteRemoveConfirm{name}` /
  `mpDeletePrompt{name}` / `wfRemovePrompt{name}`，之前只有裸按钮没有提示语）、
  `pbProjectType` 接成 project-boot 的「项目类型」小标题、`navGeneral` 与
  `importNoSelection` 删除（后者不可达：导入按钮本就 `Disabled(selected==0)`）。
  导入面板剩余英文（结果摘要四计数、失败信息、`N selected`、`N msgs`）全部迁入。
  现在 `en`/`zh-Hans` 各 749 键、占位符全对、零死键。

- **工作区 chrome i18n**（前批）：把 aux 面板四 tab（会话 / 文件 / 改动 / 提交）、Session 事实标签
  （文件夹 / 会话 / 连接 / 状态 / Agent）、Commits 空态与提交详情头、Files（刷新 / 上一级 /
  空目录 / 预览提示 / 关闭 / 无法预览）、Changes（刷新 / 无改动 / 提交信息 / 全部提交 /
  选择文件看 diff / 暂存 / 丢弃两步确认 / 关闭 / 无法 diff / 无差异），以及共享
  `loadingState` 的默认文案和侧栏「新建」按钮，全部迁入 `locales/`（en + zh-Hans，31 条新键 +
  `@auxCommitTitle{id}` / `@filesCannotPreview{message}` / `@changesCannotDiff{message}` 三个 meta）。
  新增 `TestWorkspaceChromeLocalizes`（`tt.SetLocale("zh-Hans")` 断言中文 tab 与「没有改动」空态）+
  `TestZZRenderChromeZh` 出 `workspace-zh.png`。

## 现状（已具备）

- **聊天**：用户/助手气泡、thinking、工具卡（含 gocode-mcp 工作台卡）、composer（Send/Stop）、
  历史 turn 解析、权限请求卡（workspace `viewPendingCards` + pet 面板）。
  **会话核心修复（2026-10-20）**：
  1. **历史分页**：以前请求 `get_folder_conversation_turns` 不传 `limit`，而路由的
     `clampWindow(0)` 兜底为 **1** → 历史会话**只显示最后一条**。现在固定 `limit: 100`，
     按 `turns_offset/turns_total` 累积，`turns_offset > 0` 时列表头部出现「加载更早的消息」
     并 `beforeIndex` 反向翻页（`conversationTurns` / `loadEarlierTurns` / `parseTurnsPage`）。
  2. **会话 ↔ agent 会话绑定**：`conversation.external_id` 以前只有 fork/import 会写，正常聊天
     从不写 → 重开会话历史永远为空（所有 agent）。现在 `ConnectLive` 成功后
     `rememberSessionID` 落库（首个 session 优先，不覆盖），历史端点据此读 agent 的磁盘
     transcript。
  3. **Codex transcript reader**（`src-go/parsers/codex.go`）：`$CODEX_HOME/sessions/**/
     rollout-*-<session>.jsonl`，取 `response_item`/`payload.type=message` 的 user/assistant，
     过滤注入上下文（AGENTS.md instructions / environment_context / user_instructions）。
     真机验证：260 轮会话解析出 257 轮（3 条注入上下文被剔除）。
     **2026-10-29 修**：当前 codex 写的是 **`.jsonl.zst`**（zstd 压缩），本机 1273 个压缩 vs 23 个未压缩
     —— 只匹配 `.jsonl` 会让 98% 的历史消失（上游同样只认 `.jsonl`，有测试断言 `.zst` 返回 None）。
     现在文件名比对先剥 `.zst`，`parseCodexFile` 对 `.zst` 走 `zstd.NewReader` 流式解。
  4. **prompt 串行化**：同会话的第二次发送以前会覆盖 cancel 并并发两个 prompt；现在
     `liveSession` 带队列（`promptMu`/`queue`/`runPrompts`），回合结束后按序启动；
     `CancelLive` 连同队列一起清空；用户 Stop 走 `context.Canceled` → `stop_reason:
     cancelled`（不再置 error 态）。
  5. **session/load 续接（2026-10-20）**：`AgentSession.SupportsLoadSession` 读
     `agentCapabilities.loadSession`；`ConnectLive` 用 `opts.SessionID` 或会话行里的
     `external_id` 调 `session/load`（agent 不支持或 load 失败则回落 `session/new`）。
     load 期间 agent 会**重放历史 update** —— 而历史本来就按磁盘 transcript 渲染，重放会
     双份，故 `replaying` 标记下丢弃这些 update，只发一条 `session_started{resumed:true}`。
  6. **消息级操作**：助手气泡底部的 Copy（`c.WriteClipboard` + toast）；失败回合在 composer
     error strip 上出 Retry（`lastPrompt` 记忆最近一次 prompt，一键重发）。
  7. **Gemini transcript reader**（`src-go/parsers/gemini.go`）：读
     `<GEMINI_CLI_HOME|~>/.gemini/tmp/<project>/chats/session-*.json[l]`；JSONL 的
     `$set.messages` 整表快照 + 逐条 `id` upsert，`.json` 单对象；`thoughts` → 文本、
     `toolCalls` → tool_use；过滤 `<session_context>` 前导。已接入
     `loadConversationTurns`（claude_code / codex / gemini / kimi_code / code_buddy 五种 agent
     有历史，见第 21 项）。
  8. **live 连接自动接管**：UI 重载（或另一个客户端如移动端起过会话）后 `connByConv` 是空的，
     会话会显示成 idle。现在打开会话时若本地没有连接 id，会调
     `acp_find_connection_for_conversation` 反查并接管（attach stream），状态/在飞工具卡/
     权限卡随之出现；发送成功后清掉该查询缓存。
  9. **@ 文件提及菜单**（上游 `components/chat/file-mention-menu.tsx`）：草稿尾部是
     `@<query>`（且 `@` 位于词首）时，输入框上方列出当前文件夹的文件/目录（`get_file_tree`
     maxDepth 1，目录优先、前缀过滤、最多 8 条），点击把 `@<相对路径>` 写回草稿。只处理草稿尾部
     —— `TextArea` 没有光标 API，尾部输入覆盖了实际用法。测试：`mentionQueryOf` 的 6 个用例、
     选中写回、空态。
  10. **上下文用量条**（上游 `components/chat/composer-context-usage.tsx`）：ACP
     `usage_update` 的 `{used,size}` 在 `onSessionUpdate` 里折叠进
     `SessionState.ContextUsage`（`used/size` 走指针解码，容忍 agent 发 null），快照暴露
     `context_usage`，并 emit `usage_update` 事件供实时更新。composer 底栏显示
     `12.3k / 200k · 6%`，≥80% 转 warning 色；实时流的值优先于快照。
     `formatTokens` 处理 999/1.2k/200k/1.1M。
  11. **计划卡**（上游 `components/chat/agent-plan-overlay.tsx`）：ACP `plan` 更新的
     `entries:[{content,status,priority}]` 折叠进 `SessionState.Plan` 并随快照下发（冷 attach
     也能看到），同时 emit `plan_update` 供实时更新。composer 上方渲染一张卡：`Plan 1/3` +
     每步状态图标（completed=勾·成功色 / in_progress=刷新·accent / pending=空心圆），
     high 优先级带 `!`；点表头折叠（`planCollapsed`），折叠后只留表头。
  12. **ask_user 全链路**（上游 `components/chat/ask-question-card.tsx`；上游的桥在
     codeg-mcp 里，本构建自己拥有两端）：companion 新增 `ask_user_question` 工具
     （`src-go/mcp/companion.go`）→ 调本地路由 `companion_ask_user_question`
     → `Manager.AskUser` 把调用 park 住（`questions` map + 10 分钟超时）、
     写 `SessionState.PendingQuestion` 并 emit `question_requested` →
     UI 在 composer 上方渲染问题卡（有 `options` 就一键作答，否则输入框 + Answer）→
     `acp_answer_question` 唤醒调用、emit `question_resolved`。断线时
     `dropQuestions` 让挂起的工具调用立即返回（空答案），不会吊到超时。
     **`companion_ask_user_question` 是本构建新增的本地路由**（上游 routes.txt 没有，
     因为上游把它藏在 codeg-mcp 内部）。
  13. **排队消息列表**（上游 `components/chat/message-queue-display.tsx`；上游队列在客户端，
     可重排/编辑/steer，本构建的队列在引擎里以串行化多客户端并发 prompt）：快照新增
     `queued_prompts`（按序的文本数组，永远不是 null），composer 上方逐条显示
     「Queued <text> [Remove]」；本地路由 `acp_drop_queued_prompt {connectionId, index}`
     移除一条（`Manager.DropQueuedPrompt`，越界/未知连接返回 false）。Stop 清空整个队列时
     列表同步清空。**未做**：重排、编辑、steer（上游那三项依赖客户端队列）。
  14. **会话头部 Fork**：`acp_fork` 早已实现（分叉后历史落到 sibling 行、当前行指向新
     session），但 UI 无入口。现在头部在有 live 连接时显示 Fork，成功后刷新侧栏、丢弃
     本会话累积的历史页（新 session 的 transcript 是空的）。
  15. **后台任务条**（上游 `components/chat/async-task-strip.tsx`）：上游 agent 通过
     `async_task_spawned` / `async_task_progress` / `async_task_state_update` 三种 update
     播后台任务（如「review PR」），引擎以前完全不跟踪（`acp_stop_async_task` 是空壳）。现在
     `SessionState.AsyncTasks` 按 `async_task_id` 折叠（`mergeAsyncTask`：spawn 追加一条、
     progress 只补非空字段——不覆盖已有 name/description；终态
     completed/failed/cancelled/stopped 移除该条），快照暴露 `async_tasks`，并 emit
     `async_task_update` 供实时更新。`onSessionUpdate` 走 `p.Update`（局部变量名 `p`，不是
     `disc`）。conversation 视图在排队列表**前**逐行渲染：running=刷新图标·accent / paused=时钟图标
     + name + `phase · last_tool_name` + Output 按钮（复制 `output_file_path` 到剪贴板 + toast）
     + Stop 按钮（`can_stop` 时显示，调本地路由 `acp_stop_async_task` → `Manager.StopAsyncTask`
     → `AgentSession.CallExtension("_session/async_task/stop", {async_task_id})`，按 raw 布尔出
     「任务已停止」/「智能体拒绝停止该任务」toast）。后端单测 5 个（`parseAsyncTaskUpdate` /
     `mergeAsyncTask` / 快照折叠 / 调对扩展 method / 拒绝返回 false）、UI 测试 2 个全 PASS，
     渲染 `conversation-async-tasks.png`。**未做**：上游任务完成后的输出文件内联预览（本构建无
     打开文件路由，Output 仅复制路径到剪贴板）。
  16. **实时反馈（steering）**（上游 `components/chat/feedback-dialog.tsx` +
     `feedback-notes-display.tsx` + `hooks/use-session-feedback.ts`）：用户向正在跑的回合发一条短
     note，agent 下次调用 companion 的 `check_user_feedback` 工具时读到（**pull 通道**；本 build
     不实现 ACP 的 `_session/steering` push，`native_steering_available` 恒 false）。
     - 后端 `src-go/acp/feedback.go`：`FeedbackItem`（`id/text/created_at/status/delivered_at`）；
       `SubmitFeedback` 要求 live session（否则 `ErrNoLiveSession`，handler 转成可恢复的
       `invalid_input`，前端回落为普通 prompt）；`CheckUserFeedback` 把 pending 全翻 delivered 并
       emit `feedback_consumed`（`feedback_submitted` 在 submit 时 emit）。note 是**回合级**：
       `Prompt` 开头 `ClearFeedback`。
     - 快照暴露 `feedback`（数组，pending + delivered）、`feedback_tool_available`（companion 注入
       时置 true）、`native_steering_available`（false）。
     - companion（`src-go/mcp/companion.go`）新增 `check_user_feedback` 工具 → 本地路由
       `companion_check_user_feedback`（上游把这桥藏在 codeg-mcp 内，本 build 自有两端）。
     - UI：composer 上方逐行渲染 note（pending=时钟·「waiting」/ delivered=勾·「received」）；回合
       结束后只剩未读的，切「过期」态（表头 + 「Send as message」（写入 composer 并发送）/「Dismiss」）。
       composer 底部「Feedback」入口（仅 prompting + `feedback_tool_available` + `feedback.enabled`
       开启时显示）打开内联 note 输入面板（`viewFeedbackComposer`：TextArea + Cancel/Send）。`feedback.enabled` 经
       `ensure("feedback-settings", get_feedback_settings)` 懒加载，设置变更经 firehose
       `feedback-settings://changed` 失效缓存。
     - **最小切片**：无 native push 通道（只做 pull）；「Send as message」直接发一条普通 prompt 而非
       入队；无 note 附件（`blocks`）；过期行的 dismiss 只在前端隐藏（后端保留到下一回合）。
  17. **空态欢迎页提示**（上游 `components/chat/welcome-hero.tsx` 的 `WelcomeTip`）：未选会话时的空态
     在原有产品标识 + 「New chat / Open folder…」两入口下，增加一条**会话内固定**的功能提示 pill
     （信息图标 + 一句提示）：@ 文件提及 / 排队发送 / Fork / 终端面板 / 实时反馈 / 侧栏搜索 / 待办
     看板 / 右侧面板。`welcomeTipText` 首帧挑一次索引（`time.Now().UnixNano() % 8`）后不再变，避免
     逐帧闪烁；测试固定 `welcomeTip = 0`。截图 `welcome-hero.png`。
  18. **API 重试提示**（上游 `components/chat/composer-status-strips.tsx` 的重试行）：agent 自己
     重试 API 调用（限流 / 5xx）时，composer 上方出一条琥珀色状态条「原因 · HTTP 状态 · 第 N/M 次 ·
     下次 Xs」，原地更新、随回合推进自清。数据来自 claude-agent-acp 在 `_claude/sdkMessage` 通知上
     镜像的 `{type:"system",subtype:"api_retry"}` SDK 消息（`attempt/max_retries/error/error_status/
     retry_delay_ms`，数字或数字串都容忍）。后端 `src-go/acp/api_retry.go` 解析 → `SessionState.
     ApiRetry` → 快照 `api_retry`（omitempty），并 emit `api_retry`；`agent_message_chunk` /
     `agent_thought_chunk` / `tool_call` / `tool_call_update` 视为「已恢复」清空，`Prompt` 用
     `defer ClearApiRetry` 保证回合结束必清。i18n 5 键（en/zh，含 `@apiRetryAttempt{attempt,max}` /
     `@apiRetryAttemptOnly{attempt}` / `@apiRetryHttp{status}` / `@apiRetryNextIn{seconds}` meta）。
     **未做**：codex 的 `_meta.codex.error` 与 pi 的无原因变体（本 build 无对应 agent）；重试成功的
     「已恢复」短暂确认行。
  19. **连接状态 → Reconnect**（上游 `components/chat/composer-connection-status.tsx` 的 Reconnect）：
     以前连接断开时，composer 上方只显示「The agent connection is closed.」而**没有任何恢复入口**
     （只能靠发一条 prompt 顺带重连）。现在 `status == "disconnected"` 时该条右侧出现 **Reconnect**，
     调 `Shell.reconnectLive` → `acp_connect`（同 conversationId，走 `session/load` 保留历史）→
     重新 `Attach` 流、失效 `liveconn:` / `snapshot:` 缓存。文案同步去掉「Send a prompt to reconnect」。
     i18n 加 `actionReconnect`（en/zh）。截图 `conversation-reconnect.png`。
     **未做**：上游那颗带 hover 弹层的连接「心形」图标（agent/workingDir/sessionId/错误详情 + 破坏性
     重连警告）——本 build 的连接事实已在右侧 Session 面板，重复度高。
  20. **会话导出（复制为 Markdown）**（上游 `conversation-detail-panel` 右键菜单的 `exportConversation`
     → image/markdown/html）：本 build 无保存对话框，故做**最小切片**——会话头部一个图标按钮，
     把整段会话渲染成 Markdown 写入剪贴板（`conversationMarkdown`：标题 `# `、历史回合按
     `## User` / `## Assistant` 展开 blocks 文本、live 行里的 message 同样展开、tool 行落成
     `- <title> (<status>)` 列表）。i18n 加 `actionCopyMarkdown` / `exportRoleUser` /
     `exportRoleAssistant`（en/zh）。测试：纯函数（历史 → Markdown）+ 头部按钮存在性。
     **2026-10-29 补：导出为 HTML**（`src/app/export_html.go`）。mygo 0.6 自带 `Dialog.Save`
     原生保存对话框（此前以为「无保存对话框」才退化成剪贴板）。生成**自包含** HTML：内联 CSS、无脚本、
     无外部资源，`prefers-color-scheme` 跟随明暗；正文 `html.EscapeString` 转义，工具块渲染成
     「名字 + 输入预览」卡片，实时行的工具只有状态没有输入，故状态单独成徽标。纯函数
     `conversationHTML(m, convID)` 可测，`exportHTML` 在 goroutine 里弹框 + 写盘 + toast（与
     `openFolder` 同款）。测试 2 个（含 XSS 转义断言），样例产物 `render-out/conversation-export.html`。
     **未做**：导出为图片（上游依赖前端渲染管线）。
  21. **Kimi Code / CodeBuddy / Cline / DeepSeek / Hermes / Qoder / OpenClaw / pi / Grok / OpenCode / Antigravity / Cursor transcript reader**（`src-go/parsers/kimi.go`、
     `codebuddy.go`、`cline.go`、`deepseek.go`）：本 build 实际提供 **15 个 agent**，此前只有
     claude_code / codex / gemini 有历史，其余重启后会话为空。本轮补 4 个：
     - **Kimi Code**：`<KIMI_CODE_HOME|~/.kimi-code>/sessions/<workDirKey>/<sessionId>/agents/
       main/wire.jsonl`（目录-per-session；bucket 名不透明，按 `<sessionId>` 走一层子目录找）。
       `turn.prompt`（`input[].text`）开 user 回合；`context.append_loop_event` 里
       `content.part`（`text` / `think`）与 `tool.call`（`toolCallId`/`name`/`args`）累积成**一个**
       assistant 回合；`tool.result` / `step.*` / `usage.record` / `context.append_message` 跳过。
     - **CodeBuddy**：`<CODEBUDDY_CONFIG_DIR|~/.codebuddy>/projects/<encoded-cwd>/<sessionId>.jsonl`
       （借 Claude 的目录布局，但行 schema 是 OpenAI Agents SDK "items" 形状）：`message` 的
       `input_text`/`output_text`、`reasoning`（`rawContent[].text`）、`function_call`
       （`callId`/`name`/`arguments` 为 **JSON 字符串**，预览前先解析）。
     - **Cline 3.x**：`<CLINE_DIR|~/.cline/data>/sessions/<id>/<id>.messages.json`，Anthropic 形状
       blocks（`text`/`thinking`/`tool_use`/`tool_result`）。真实 prompt 被包在
       `<user_input>` / `<user_command>` 里 → `clineCleanUserText` 解包（`<mode_notice>` 整块丢弃）；
       只带 tool_result 的 `role:"user"` 记录跳过；`metadata.displayOnly` 的失败回显跳过。
       解包器按「从开标签的 `>` 之后再找闭标签」实现，字符串严格收缩（上游在此踩过指数级死循环）。
     - **DeepSeek Harness**：`<DEEPSEEK_ACP_SESSIONS_ROOT|$DSH_HOME/sessions|~/.dsh/sessions>/
       <munged cwd>/<sessionId>/session[.vN].jsonl[.zstd]`。**生成代际**：先比编码（zstd 优先）、
       再比代际（数值最大）；`.zstd` 是多个完整 Zstandard 帧串接，用
       `klauspost/compress/zstd` 流式解码（**新增直接依赖**，版本沿用依赖树里已有的 v1.18.0）。
       `user/message` 仅收 `data.source.kind=="user"`（plugin 注入/压缩摘要跳过）；
       `assistant/message` 读 `data.message.content[]`（`text`/`reasoning`/`tool-call`）；
       `assistant/chunk` / `*-chunks` 跳过（与 assembled message 重复）。
     - 四者都按「user 消息开回合、后续 assistant 事件累积」分组成交替回合；时间戳为 ms epoch。
     - **测试**：`kimi_test.go` / `codebuddy_test.go` / `cline_test.go` / `deepseek_test.go` 用
       **合成样本**（字段名严格照上游 fixture；deepseek 用例真的 zstd 压缩，并覆盖代际选择）。
     - **Hermes**：`<HERMES_HOME|~/.hermes>/state.db`（SQLite，只读 `mode=ro` 打开，不动活跃写入）。
       `messages` 表按 `id` 升序（**不是** timestamp），`active = 0` 是 rewind 掉的行要跳过；
       `timestamp` 是 **epoch 秒（REAL）**；多模态 `content` 带 `\x00json:` 前导 + JSON parts 数组；
       `tool_calls` 是 OpenAI function-call 形状（`arguments` 为 JSON 字符串）。`role:"tool"` 结果行
       与 `system` 行跳过。
     - **Qoder**：存储**整套沿用 Claude**（`<QODER_CONFIG_DIR|<homeRoot>/.qoder>/projects/
       <encoded-cwd>/<sessionId>.jsonl`，行格式同 Claude），所以直接复用 `parseClaudeFile`，只换目录
       解析；`homeRoot` = `QODER_CLI_HOME` → `GEMINI_CLI_HOME` → `$HOME`。
     - **OpenClaw**：`<~/.openclaw/agents>/<agent>/sessions/<sessionId>.jsonl`（另有
       `<id>.jsonl.reset.<ts>` 轮转档）。文件是**树不是流**：每行带 `id` / `parentId`，fork 会在同一
       parent 下写第二个孩子；`type:"session"` 头带 cwd（跳过）。content 是 **Claude 形状**，直接复用
       `contentBlocks`。**取最长 root→leaf 链**当主分支（上游把一个分支列成一个会话，本单 id reader
       取最长）。user 文本要剥三种前缀：`Sender (untrusted metadata): ```…``` `、`[Tue … GMT+8]`、
       `[Working directory: …]`。
     - **pi**：sessions 目录三级优先级 `PI_CODING_AGENT_SESSION_DIR` → agent 目录 `settings.json` 的
       **绝对** `sessionDir` → `<agent dir>/sessions`（agent dir = `PI_CODING_AGENT_DIR` 或
       `~/.pi/agent`，按 pi 的 tilde 规则展开）。文件 `<sessions>/--<dashed-cwd>--/<ts>_<uuid>.jsonl`，
       首行 header `{"type":"session","id":…}` 的 `id` 才是 external id（文件名只有 uuid 前 8 位，作
       提示）。content 近似 Claude 但两处不同：thinking 的载荷键是 **`thinking`** 不是 `text`；
       `toolCall.arguments` 是**对象**不是 JSON 字符串。
     - **Grok**：`<GROK_HOME|~/.grok>/sessions/<encoded-cwd>/<sessionId>/updates.jsonl` —— 是
       **ACP `session/update` 流**（和本 app 实时消费的线格式同构）。`update.sessionUpdate` 为
       `user_message_chunk` / `agent_message_chunk` / `agent_thought_chunk` / `tool_call` /
       `tool_call_update` / `turn_completed`。**chunk 是增量**：同种 chunk 必须拼成一个 block
       （拼完再 trim，逐块 trim 会吃掉中间空格）。两处特例：`_meta.hideFromScrollback` 的 user chunk
       是 Grok 自己的提醒（其 TUI 也不显示）要跳过；无 assistant 介入时 user chunk 延续当前 user
       回合（"文字+图片"是一个气泡）。时间戳是 unix **秒**。工具名优先取
       `_meta["x.ai/tool"].name`，回退 `title` / `kind`。
     - **OpenCode**：`<XDG_DATA_HOME|~/.local/share>/opencode/opencode.db`（SQLite，只读）。
       `message(id, session_id, time_created, data)` + `part(id, message_id, time_created, data)`，
       两个 `data` 都是 JSON。`message.data.role` 决定回合；`part.data.type` 为 `text` / `reasoning` /
       `tool`（`{tool, callID, state:{input}}`）。**`synthetic: true` 的 text part 必须丢**——那是
       OpenCode 注入给模型的提示（plan/build 提醒、压缩后的 "continue"），渲染出来会把整段系统提示
       塞进用户气泡。`system` / `tool` 角色的 message 不单独成回合。
     - **Antigravity**：`<GEMINI_HOME|~/.gemini>/antigravity-acp/conversations/<id>.db`（SQLite，
       单表 `steps(idx, step_payload)`）。每行是 `gemini_coder.Step` **protobuf**，本仓自带一个**最小
       wire 解码器**（`protoFields`/`protoBytesAt`/`protoAllBytesAt`/`protoIntAt`，只处理 varint 与
       length-delimited）。只声明转录需要的 arm（真实有 ~130 个）：`metadata=5`（`created_at=1`）、
       `user_input=19`（`query=1`/`user_response=2`/`items=3`）、`planner_response=20`
       （`response=1`/`thinking=3`/`tool_calls=7`）。**新建会话的 db 是 0 字节**（schema 还没写），
       `no such table: steps` 是正常态，读作空会话。
     - **Cursor**：`<CURSOR_CONFIG_DIR|$XDG_CONFIG_HOME/cursor|~/.cursor>` 下的
       `acp-sessions/<uuid>/store.db`（另有 `chats/<md5-of-cwd>/<uuid>/store.db`）。是
       `blobs(id, data)` + `meta(key,value)` 的**内容寻址 DAG**：`meta` 的 key `"0"` 是**十六进制
       编码的 JSON**，`latestRootBlobId` 指向根 blob；根 `8: turns[]` → 每回合 `1: user_message` +
       `2: steps[]` → 每 step 是 oneof `1: assistant_message{1:text}` / `2: tool_call{oneof}`
       / `3: thinking_message{1:text}`。blob id 线上是裸字节、表里是小写 hex，每次查表先 hex 编码。
       step 的文本是**增量**要拼接（与 grok 同理）。tool_call 的 oneof 字段号 → 工具名有一张映射表。
     - **未做**：usage/上下文窗口统计、sub-agent 统计、tool_result 配对、title 解析、DeepSeek 的
       attachments 与 delegation、Hermes 的 session 统计、Qoder 的 slash-command 改写、OpenClaw 的
       多分支列举、pi 的 session_info 标题与 model_change、Grok 的 `use_tool` 信封剥离与 plan 状态、
       OpenCode 的 `task`→Agent 卡改写、Cursor 的附件与 turn-span 时钟。
       **15/15 agent 全部有历史** —— 本 build 提供的每个 agent 都能在重启后回看自己的会话。
     - **端到端形状已锁**：`TestLoadConversationTurnsReadsRealTranscript`（handlers）走
       「会话行 agent_type + external_id → reader → `convTurn.raw["blocks"]`」整条链；
       `TestHistoricalTurnRendersBlocks`（app）断言 UI 侧把 `type:"text"` 渲染成气泡、
       `type:"tool_use"` 渲染成工具卡（截图 `render-out/history-tool-card.png`）。15 个 reader 共用
       这一种 block 形状，所以这两个测试守的是**全部 15 个 agent 的历史渲染**。
     - **真机验证工具**：`src-go/parsers/realdatasmoke_test.go`（默认 skip，`GOCODE_PARSER_SMOKE=1`
       才跑）对 15 个 agent 各取样最多 5 个真实会话调 reader，只报「读出了多少回合」；没数据或
       会话本身退化（Gemini 只有 `<session_context>` 前言、OpenCode 的 assistant 无 part）记为 skip
       而不是失败。本机实测：claude_code 236 轮 / **codex 257 轮（来自 .jsonl.zst）** /
       deepseek 4 轮 / open_code 1 轮，其余 agent 无数据。
  22. **mygo 升级到 0.6.3 + 拖放附件**（上游 `message-input` 的 paste/drop files）：
     - `github.com/egoist/mygo v0.5.5 → v0.6.3`（用户要求「有新版本就升级，新特性就用上」）。
       顺带 `x/crypto 0.51 → 0.57`、新增 `x/net` 间接依赖；`go build ./...` 与全量测试通过。
     - 免费获得的改进：diff 两侧同宽换行、代码编辑器长行换行、图表缩放平移、布局缓存/绘制裁剪。
     - **新能力落地**：composer 整个 dock 调 `composer.DroppedFiles()` —— 从资源管理器/Finder
       拖文件进来即 `addAttachment` 暂存（与回形针同一条路径）。测试用 `Tester.DropFiles`。
     - **未用**：`ListState.CellSelect`（本 build 无 `ui.Table`）、`MaximizeButton`（窗口用系统标题栏）。
  23. **Changes 面板多选 + 批量操作**（用上 mygo 0.6 的 `ListState.Selection` / `DragSelect`）：
     Changes 列表从 `Scroll+Column` 改为 `ui.List`，行以文件路径为 key，支持点击 / Ctrl(⌘)-点击 /
     Shift-点击 / 拖拽框选 / Ctrl+A。选中后表头出现 `Stage N` / `Discard N`：
     批量暂存一次 `git_add_files` 多文件，批量丢弃逐文件 `git_rollback_file`（**两步确认**，与单文件
     一致）。行渲染用**单个 `ui.RichText`**（状态码 + 文件名两段着色），选中行文字取 `t.AccentText`
     —— mygo 的 list 行是内容宽度、第二个子元素会被裁、选中行是深色 accent 背景，这三点必须按此写法
     （详见 memory 的「mygo List 行的四个坑」）。7 个新 i18n 键，`stageFile/discardFile` 合并为
     `stageFiles/discardFiles(loc, …)` 并本地化。测试 2 个 + 截图 `changes-multi-select.png`。
  24. **图标按钮统一 tooltip**：`iconButton`（55 处调用）的 label 同时作为悬停提示
     （`Element.Label(x).Tooltip(x)`）；另给 5 处绕过它的图标控件补上：附件移除 ×、Git 操作「⋯」
     （Session 面板与 Changes 面板各一处）、会话标签关闭 ×、终端标签关闭 ×、toast 关闭 ×。
     测试 `TestIconButtonShowsTooltipOnHover`：悬停 700ms（tooltip dwell 是 600ms）后断言文本多出一条
     —— **注意** `HasText` 会把 a11y label 也算进去，所以必须比较出现次数而不是有无。截图
     `render-out/icon-tooltip.png`。
  25. **Commits 面板改用 `ui.List`**：原先是 `Scroll+Column` + 手写 `commitSel` 高亮，现在行以
     hash 为 key，`Selected` 给出点击与**方向键**单选，详情面板每帧跟随 `commitsIdx`。踩到并规避了
     两个 List 坑：行是内容宽度（hash 与两行文字都给显式 Width，不能靠 Grow），选中行是深色 accent
     底（文字取 `t.AccentText`）。测试 `TestAuxPanelCommitsKeyboardNav`（点击 → Down → 详情换成
     第二条提交）。截图 `render-out/aux-commits-list.png`。
     **顺带修了一个测试脆弱点**：终端标签的关闭按钮现在带 tooltip「Close terminal tab: {title}」，
     而 `HasText` 是**子串**匹配 —— tooltip 一露头，`TestTerminalTabCloseOthersAndAll` 里
     `!HasText("Terminal 2")` 就永远不成立（全量跑时偶发超时）。断言前 `tt.Move(2,2)` 把指针移开。
  30. **导入扫描支持 Codex**：`scan_importable_sessions` 的注释原来写着「其他 agent 的 parser 还没移植，
     所以贡献为空」—— 15 个 reader 早就写完了，这句话已经不成立。现在补上 `ListCodexConversations()`
     （`src-go/parsers/codex_summary.go`）：只读**每个 rollout 的头部**（`session_meta` 给 id/cwd/时间戳，
     之后几行给首条 user 提示词当标题），拿到 id+标题即提前停；**8 路并行**。
     本机实测 1299 个 rollout（含 1273 个 `.jsonl.zst`）从 **25.7s 降到 590ms**，1296 条有 cwd、
     981 条有标题（其余会话本身没有 user 消息）。`MessageCount` 留 0（精确计数要全量解析）。
     导入只写 `conversation` 行（agent_type + external_id），历史仍由 reader 按需读。
     测试：`TestListCodexConversations` / `TestBaseNameOfPath` / `TestScanImportableCodexSessions`。
     **2026-11-01 补 OpenCode**：`ListOpenCodeConversations()`（`opencode_summary.go`）—— 它比文件型
     agent 简单得多：`session` 表直接有 `id / directory / title / time_created / parent_id`，
     **一条 SQL 就是全部**，不解析任何 transcript。`parent_id` 非空的子会话跳过（属于父会话）。
     标题是 opencode 的默认值 `New session - <时间戳>` 时，取首条**非 synthetic** text part 作为标题
     （`LIMIT 40` 条 part 内找不到就保留默认）。本机实测 8 条会话，标题正确（如
     `修复git插件，工作区过滤包含.git目录的子文件夹`）。
     **2026-11-01 补 Gemini**：`ListGeminiConversations()`（`gemini_summary.go`）——
     `<gemini>/tmp/<project>/chats/*.jsonl`，cwd **只存在于同目录的 `.project_root` 边车文件**里
     （chat 头只有 `projectHash`），头行给 `sessionId`/`startTime`/`lastUpdated`/`kind`；
     `kind` 非 `main` 的子会话跳过；标题取首条**非 `<session_context>` 前言**的 user 消息。
     本机 5 条会话实测：cwd/id/时间都对，标题全空 —— 因为这些会话本身只有前言（与第 21 条结论一致）。
     **2026-11-01 再补 pi + OpenClaw**：这两家的会话头是**同一个形状**
     （`{"type":"session","version":3,"id":…,"timestamp":…,"cwd":…}`），所以抽了个共用读取器
     `readSessionHeader`（`session_header.go`，一遍扫完头行 + 首条 user 文本，拿到即停，192KB 上限）。
     `ListPiConversations()`（`pi_summary.go`，id 在文件头、目录是 `--<dashed-cwd>--`）与
     `ListOpenClawConversations()`（`openclaw_summary.go`，跳过 `sessions.json` 索引与 `.reset.*` 轮转档）。
     **2026-11-01 再补 Grok**：`ListGrokConversations()`（`grok_summary.go`）—— 最省的一个：
     grok 在每个会话目录写了 `summary.json` 边车，**cwd / 标题 / 模型 / 分支 / 时间全在里面**，
     **完全不读 transcript**。`session_kind == "subagent"` 的子代理跳过；边车缺失但有
     `updates.jsonl` 的会话仍然列出（历史可导入，只是没有元数据）；只有目录没 transcript 的不列。
     顺带把 `model` / `git_branch` 也填进 summary（其余 lister 拿不到这两项）。
     **2026-11-01 再补 Hermes**：`ListHermesConversations()`（`hermes_summary.go`）—— SQLite，
     一条查询就够。两个关键点照上游：`cwd` **列**对 ACP 会话是 NULL，工作目录在 `model_config`
     JSON 里（用 `json_valid` 兜住非 JSON 值，否则整条 SELECT 会挂）；`started_at`/`ended_at` 是
     epoch **秒**（REAL）。archived 隐藏、无真实消息的跳过、`parent_session_id` 子会话跳过。
     **2026-11-01 再补 Qoder + Kimi**：
     - `ListQoderConversations()`：Qoder 与 Claude **存储同构**，所以把 Claude 的遍历器参数化成
       `listClaudeStyleConversations(projectsDir, agentType)`，Qoder 只是换个目录 + 改写 agent_type。
     - `ListKimiConversations()`（`kimi_summary.go`）：Kimi 自带 `session_index.jsonl`
       （`{sessionId, sessionDir, workDir}`）—— 索引给 id 与工作目录（桶目录名是哈希，不是路径），
       每个会话的 `state.json` 给 `title`/`createdAt`/`updatedAt`。**不读 transcript**。
     **2026-11-01 收尾：最后四个补齐，15/15 全覆盖**（`collectLocalSummaries` 现在接满 15 个）：
     - `ListClineConversations()`（`cline_summary.go`）：`sessions/<id>/<id>.json` **manifest 就是全部**
       —— `cwd` / `workspace_root` / `prompt` / `model` / `metadata.title` / `metadata.git.branch` /
       `started_at` / `ended_at`。子代理只写 `db/sessions.db` 行、**没有 manifest**，所以「只走 manifest」
       天然只列根会话。`workspace_root` 是 cwd 缺失时的回退（从家目录起的任务）。
     - `ListCodeBuddyConversations()`（`codebuddy_summary.go`）：Claude 的目录布局 + **OpenAI Agents
       记录类型**（`message`/`reasoning`/`function_call`），所以 `parseClaudeSummary` 吃不了，单独实现。
       标题按 CodeBuddy 自己的 `getEffectiveSessionTitle`：`custom-title` > `ai-title` > `topic` >
       首条 user 文本，**同种取最后一个**（记录只追加不改写）；`providerData.isMeta` 的注入 user 记录
       既不算消息也不当标题。
     - `ListCursorConversations()`（`cursor_summary.go`）：blob DAG。meta key `"0"`（hex JSON）给
       `name`/`createdAt`/`lastUsedModel`/`isSubagent`，ACP 的 `meta.json` 边车给 `cwd`/`title`，
       根 blob 解 `21 tracked_git_repo_branches`（repo_path + branch）/ `9 workspace_uris` /
       `26 conversation_started_timestamp_ms` / `8 turns`（只数条数）。`chats/<md5-of-cwd>/` 的桶名
       不可逆，所以那边的 cwd **只能**来自 DAG。子代理跳过；没有标题记录时只解第 0 轮取首条提示。
     - `ListAntigravityConversations()`（`antigravity_summary.go`）：每会话一个 SQLite `steps` 表；
       cwd 只来自 `<id>.meta` 边车（**故意不回退**到 Gemini home，未知就该是未知），标题取首个
       `user_input`，时钟与 `metadata.model_info`（`20 display_name` 优先）来自各 step 的 metadata。
       刚创建还没写 schema 的库（`no such table: steps`）读作空会话而不是错误。
     **性能**：15 个 lister 全接上后本机实测 **1399 个会话 / 6 个 agent**。第一版要 **10.95s** ——
     瓶颈是 CodeBuddy（58 个文件 **8.98s**，其中一个是 146MB 的会话）。原因是逐行 `json.Unmarshal`。
     改成**两段式**：头 400 行解码拿 cwd/id/model/标题/首条提示，之后**只做字节计数**
     （`"type":"message","role":"user"|"assistant"` 在记录里紧邻，`isMeta` 仍排除注入），再**8 路并行**
     → **689ms**。计数与 `grep` 核对完全一致（3267 行 − 122 条注入 = 3145）。整条扫描 **10.95s → 1.71s**。
     **测试隔离**：导入测试现在用 `pinAgentHomes(t, home)` 一次钉住 14 个环境变量
     （`CODEX_HOME`/`XDG_DATA_HOME`/`GEMINI_CLI_HOME`/`GEMINI_HOME`/`PI_CODING_AGENT_SESSION_DIR`/
     `GROK_HOME`/`HERMES_HOME`/`QODER_CONFIG_DIR`/`KIMI_CODE_HOME`/`CLINE_DIR`/`CODEBUDDY_CONFIG_DIR`/
     `CURSOR_CONFIG_DIR`/`DSH_HOME`/`DEEPSEEK_ACP_SESSIONS_ROOT`）。踩过一次：`TestImportSelectedAndLocal`
     没钉，本机 `CODEBUDDY_CONFIG_DIR` 指向宿主目录，断言「1 个文件夹」直接变成 21。
     **注意**：本机 `CODEBUDDY_CONFIG_DIR` 被宿主设成 `.workbuddy-ai`，所以 codebuddy 的 lister 实际在扫
     宿主的会话记录 —— 这是环境变量本身语义（CodeBuddy 自己也会这么解析），不是 bug。
     **标题记录可以在任意位置**（中途 `/rename`），所以尾部通道对**非消息行**仍会找 `"type":"custom-title"` /
     `"ai-title"` / `"topic"` 并只解那一行 —— 消息行先走「字节计数即返回」的快路径，所以这个正确性
     几乎不要钱（661ms）。测试 `TestCodeBuddyTitleAfterHeadPass`（把改名放到第 410 行之后）。
     UI 侧补了 `TestImportPickerListsEveryAgent`：一次扫描结果里放 6 个 agent 的会话，断言选择器把
     每个 agent 的名字与标题都渲染出来、点行能选中。截图 `render-out/import-picker-agents.png`。
  29. **⌘K 能搜会话了**：此前 `toggle_search` 只打开命令面板，而面板只列**命令** —— 15 个 agent、
     几十个会话的场景下，按名字找不到自己的对话。现在 query 非空时先把**当前文件夹**里标题命中的会话
     排在命令前面（最多 8 条，右侧显示 agent_type），回车即 `openConversationRow` 开成标签。
     查已缓存的 **`all-convs`**（`list_all_conversations`，标签条每帧已经在暖它）→ **跨文件夹**也能搜到；
     缓存未热时回退到 `convs:<folderID>`。全程 `peek`，不触发请求；空 query 时不列会话。
     测试 `TestPaletteSearchesConversations`（空 query 不列 → 有 query 命中 → 点击后 `openTabs` 为 1）
     与 `TestPaletteSearchesEveryFolder`（跨文件夹命中）。
     截图 `render-out/palette-conversation.png`。
  28. **i18n 收尾扫描**：脚本扫全仓 `ui.Text/Button/PrimaryButton/Textf(c, "…")` 的**字面量**，
     只剩 10 处，其中 7 处本就不该翻译（`\u25CF` 状态点、`GoCode v0.1.0` 品牌版本、`PR` 徽标、
     `%.1f KB` 数字格式）。真正遗漏的 3 处已迁入 locales（en + zh + meta）：
     `browserPorts` / `browserPublicHost` / `agentCustomBadge`。
     顺带修了个渲染问题：`ui.Textf(c, "ports: %v", ports)` 会把切片打成 `[3000 3001]`，改为 join。
     测试 `TestLocalizedUILabels`（中英各三例）。既有 `TestSettingsAgentsCustomCreateAndDelete`
     断言的是旧格式 `custom:team_agent`，已随新文案改为 `custom: team_agent`。
  27. **两处「静默失败」修掉**：`actionMsg` 有 78 处写入，但只有**工作台路由**（`viewActionOrError`）
     和 **Changes 面板**的提交框渲染它。
     - 我上一轮的 HTML 导出把结果写进 `actionMsg` —— 而**会话视图根本不渲染它**，等于导出成功/失败
       都看不见。改用 `notify`/`notifyErr`（toast；`enqueueToast` 有独立 `toastMu`，goroutine 里安全）。
     - **设置页**（appearance / browser / version-control）保存失败、token 探测结果同样写进
       `actionMsg` 而从不显示。在 `viewSettings` 的页面容器顶部加一条提示（muted、可换行），并记录
       `settingsMsgPage`：**切页就清掉**，避免上一条消息挂在另一页的表单上方。
       测试 `TestSettingsShowsActionMessage`（显示 → 切页后消失）。截图
       `render-out/settings-action-message.png`。
     - **第三处**：composer 的 mode / model / 权限切换（`dispatchConfig`）失败时写 `errorMsg`，
       而 `errorMsg` 同样只由 `viewActionOrError` 渲染 —— 聊天里切模型失败也是静默的。改用 toast。
       既有测试 `TestComposerRendersModelAndModeSelectors` 原本断言 `s.errorMsg`，改成断言
       **渲染出来的 toast 文本**：队列每帧被 `drainToasts` 抽干，所以只能看渲染结果。
     - **第四处**：自定义 skill 的创建/删除把后端错误整个丢掉（`_, _ = s.B.raw`），创建失败还会把
       用户刚输入的 id 清空 —— 表现为「按钮没反应」。现在失败即 `notifyErr` 并**保留输入**；
       `newSkillID` 的写入补上了 `s.mu`（原来在 goroutine 里裸写）。
       测试 `TestCreateCustomSkillFailureKeepsInputAndToasts`（不渲染 → 队列不抽干，可直接轮询）。
       注：删除不存在的 skill 后端是**幂等 200**，没有可触发的失败，故只测创建。
  26. **导出动作进命令面板 + 修掉两处子串断言脆弱点**：`Copy as Markdown` / `Export as HTML`
     加进 ⌘K 目录（`PaletteChat` 分组，无选中会话时 no-op），测试断言两者出现在面板里。
     顺手修了两个**一直存在、被随机因素掩盖**的 flaky：
     - 空态提示是**每次会话随机**选的（`UnixNano % len(tips)`），而 `HasText` 是**子串**匹配 ——
       `"Sending while a turn is running…"` 里含 `Send`，于是 `TestTerminalPanelCollapsedByDefault`
       的 `!HasText("Send")` 会按概率失败。修法：终端相关断言改**精确匹配**（`countText`），
       并在两个测试 helper 里把 `s.welcomeTip` 钉成 0（`welcome_tip_test.go` 早就这么做了，
       helper 里漏了）。
     - 另见第 25 条的 tooltip 与 `HasText` 冲突。

- **Markdown**：✅ 已补（`src/app/markdown.go`）——围栏代码块、标题、有序/无序列表、引用、分隔线、
  行内 code/粗体/斜体/链接。
- **侧栏**：空间/最近/置顶分段切换（UI 优化点 1）。
- **底部状态栏**：connected + 版本 + MCP 健康 + 待办计数 + 更新提示（UI 优化点 2）。
- **设置**：21 个子页（general/appearance/agents/model-providers/version-control/mcp/
  quick-messages/web-service/servers/shortcuts/skills/skill-packs/experts/science/
  office-tools/chat-channels/collaboration/computer-use/browser/logs/system）。
- **工作台**：automations / tasks（To-dos）/ forge（Repositories）/ canvas。
- **其它**：files / changes（diff）/ terminal / pet / login / import-sessions / project-boot。

## 桌面精灵（2026-11-01）

上游 `src/app/pet` 的精灵子系统落地，**不再是占位提示**：

- **窗口**：独立 mygo 窗口（`Frameless` + `Transparent` + `AlwaysOnTop` + `SkipTaskbar`，
  先 `Hidden` 建窗、精灵图解码完再 `Show`），所以 pet 不再是「主窗口里的一个标签页」，
  而是真的浮在桌面上。位置 / 尺寸 / 置顶写回 `pet_save_window_state`，`enabled` 让下次
  启动恢复（`Shell.RestorePetWindow`）。
- **动画**：照 `lib/pet/animation.ts` 的 8 列 × 192×208 帧格与逐帧时长表；idle 每 8–15s
  随机插入 waving/jumping flourish；one-shot 按 `PET_ONESHOT_LOOPS` 播 2–3 轮后回到环境态。
  帧调度用 `c.Now()` + `c.After`，不额外开定时器。
- **交互**：拖动 = `DragWindow`，`OnMove` 的位移驱动 running_left/right 行走动画；
  右上角会话角标（waiting > error > running，点击跳 Pet 页会话列表）；右下角 ⋯ 出**原生
  右键菜单**（跳跃 / 挥手 / 放大 / 缩小 / 重置 / 置顶 / 打开 Pet 页 / 关闭）。
- **管理页**（Pet → Sprite）：宠物列表、当前宠物徽标、两步删除、尺寸滑杆、打开/关闭窗口、
  添加（文件对话框 → base64 → `pet_add`）、从 Codex 导入（`pet_import_codex`）。
- **事件**：`pet://oneshot` / `pet://state` / `pet://active-changed` 经 firehose 驱动
  （`stream.go` 的回调现在携带通道载荷）。

**仍然省略**（诚实记录，非遗漏）：

- **marketplace 不抓**：上游 `pet_marketplace_*` 是第三方美术素材目录，后端保留诚实降级
  （空页 / unavailable），UI 不提供入口。
- **悬停挥手不做**：上游靠 Rust 的全局光标 watcher（`spawn_pet_hover_watcher`）跨窗口检测
  hover；mygo 没有等价能力，改用 ⋯ 菜单里的「挥手」，并在悬停时显示 ⋯ 按钮。
- **单击跳跃让位给拖动**：`DragWindow` 的按下即开始拖窗，会吞掉左键 click，所以
  「点一下跳一下」改成菜单里的「跳跃」+ 后端 one-shot。
- **无 spritesheet 校验/裁剪 UI**：添加时不做尺寸预检（后端只校验 1KB–16MB 与 base64），
  非 Codex 尺寸的图会按 8 列切、行数按高度推。

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
   （Quick Messages 全 CRUD + 校验/反馈/帮助文案已完成 2026-10-16，提交 ea51817；
   Agents 版本探测反馈 + 自定义 agent 表单/校验已完成 2026-10-20，见下；其余页待补。）
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

1. ~~设置页深度续（P1-8）。~~（已完成：Version Control b0992e4；Quick Messages ea51817；
   Agents 2026-10-20。）
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
  `Close / Close Others / Close All`、尾部「新建会话」按钮；经 `list_opened_tabs` /
  `save_opened_tabs` **按 CAS 版本持久化**（切 tab 写回 `is_active`，重启/多窗口一致）。
  上游 `components/tabs/tab-bar.tsx` + `tab-item.tsx` + `stores/tab-store.ts`。
  Terminal 不在条上：它单独是底部可折叠面板（见下条），不再是主区的一个 tab。
  **未做（有意）**：上游的 IDEA 式分屏组（`groupLayout` / 跨组拖拽 / tile 模式）与
  设备本地 draft tab——两者都是独立大子系统，且非「条本身可用」所必需；本 build 的
  「新建会话」已直接 `create_chat_conversation`，故无需 draft 概念。
- [x] **Git 操作面**：上游是 commit / merge / stash / push 四个**独立窗口**
  （`src/app/commit|merge|stash|push`）。本 build 做**整页路由**（`workbench`），
  后端 `git_log` / `git_branch` / `git_commit` / `git_diff` / `git_merge` / `git_push` /
  `git_stash` 已就绪。
  **已完成（本次）**：`git_ops.go` 四个整页路由 `git-commit` / `git-stash` / `git-push` /
  `git-merge`，入口是 aux 面板 Changes / Commits 头部的 «Git 操作» 菜单
  （commit / push / merge / stash）。
  - Commit：分支 + 作者事实；tracked / untracked 分组（tracked 默认全选、untracked 默认折叠且不选）、
    组复选框与全选 / 清空、目录树、选择计数；右侧原生 `editor.DiffView`（HEAD ↔ 工作区并排，
    二进制 / 空文件提示）；未合并冲突横幅 + Open Merge；提交信息 + `Commit (n)` /
    `Commit and Push (n)`（`git_commit` 暂存指定文件后提交，可选 `git_push`）；刷新。
  - Stash：创建（message + keep-index）、列表（ref / message / branch·date）
    + Pop / Apply / Drop / Clear（Clear 二次确认）。
  - Push：`branch → remote/branch` 目标头（多 remote 用原生 Select 切换）；`git_log` 按
    branch/remote 只列 `pushed === false` 的提交，展开为文件夹树（status / +adds / -dels）；
    点文件取 `<hash>~1` ↔ `<hash>` 原生并排 diff；无 remote / 无未推送 / 新分支空态；
    `Push (n)`（`git_push`，首次推送自动设 upstream）。
  - Merge：选分支合并（`git_merge`）；冲突时进冲突态——`git_list_conflicts` 列出未合并
    路径 + Abort / Continue（`git_abort_operation` / `git_continue_operation`），
    由 `git_has_merge_head` 门控。
  - **深度补齐（2026-10-20）**：Commit / Push 从最小切片升级为上述工作台；缓存按
    branch/remote/文件分键并按前缀失效，换 remote 重置展开与预览。
  - **仍省略**：hunk 级 stage/unstage 与行内操作、文件/目录右键 rollback·delete·add-to-VCS、
    图片 diff（ImageDiffView）、force-with-lease、推送进度流、提交信息历史 / AI 生成、
    提交卡片上的相对时间（这里用 `YYYY-MM-DD`）。
- [x] **分支选择器**（composer 上的 branch picker，上游 `conversation-branch-tag`）。
  **已完成（本次）**：composer 输入框下方的**分支 chip**（`viewBranchChip`）+ **Branches 整页路由**
  （`git-branches`，也从 «Git 操作» 菜单进）。chip 显示当前 checkout 的分支名，点击进 Branches 页；
  Branches 页：当前分支 + worktree 数事实、新建分支表单（`git_new_branch`）、本地分支列表
  （当前项打勾 + current 徽标；其他项 Switch `git_checkout` / Merge `git_merge` / Delete
  `git_delete_branch`，Delete 二次确认并给 Force delete）。folderless chat 不显示 chip（无分支）。
  **深度补齐（2026-10-20）**：
  - worktree：新建表单（分支名 + 目录，默认预填同级 `repo-branch` 路径；`git_worktree_add`
    成功后经 `open_worktree_folder` 注册为文件夹并选中打开）、Other worktrees 区（Switch 经
    `resolve_worktree_folder` 导航到该 checkout，未注册则先注册）、Remove / Remove + delete branch
    两步确认；git 报 `--force` / `not fully merged` 时升级 Force remove，主工作树只给 Switch。
  - rebase：每个非当前本地分支一个 Rebase（`git_rebase`）。
  - 分组：本地分支按 `/` 前缀压缩成可折叠树（组头显示叶子数，worktree 持有的分支不再给普通
    Delete，与上游 `branchLeafActions` 一致）。
  - 远程分支：**按 remote 分组**（组头 remote 名 + 数量），叶子显示短名；Checkout 按短名检出为
    root 工作树的跟踪分支（上游 isRemote 规则；不会误撞同名本地 worktree 分支）。
  - **叶子操作补齐（2026-11-04，对齐上游 `runLeafAction`/`branchLeafActions`）**：本地叶子
    +Pull（`git_update_branch`，不动工作树：检出分支真 pull、其余从上游 ff；按 updated_files
    本地化 toast）与 +Push（**打开 Push 工作台并预选该分支**——`pushBranchOverride`，菜单里的
    Push 入口清 override）；远程叶子 +Pull（`git_update_branch isRemote:true` 更新跟踪引用）与
    +Delete remote（两步确认 → `git_delete_remote_branch`）。
  - **worktree 创建后自动开新会话**（上游 openWorktreeFolder + fresh conversation）：
    `git_worktree_add` → `open_worktree_folder` → `create_conversation` → `openConversationTab`；
    开会话失败不影响 worktree 本身。
  - 仍省略：远程叶子的 Merge / Rebase（上游对远程引用也提供；本 build 只在本地叶子给，
    `git_merge`/`git_rebase` 对 `origin/x` 未验证）、上游 Push 窗口的分支下拉（本 build 用
    预选 override 代替）。
  **最小切片**：上游 `branch-dropdown.tsx`(860 行) 的远程叶子合并/变基与 Push 窗口分支下拉未复刻。
- [x] **Terminal 移到底部面板**（本次）：上游 terminal 是「底部可折叠面板 + 多
  terminal tab」，现在改成底栏——`viewWorkspace` 外层由 `ui.Row` 改为 `ui.Column`，
  三栏（sidebar / detail / aux）`Grow(1)` 占满上部，下方始终挂一个
  `viewTerminalPanel`（Surface 背景的整宽列）。面板头常驻（▸/▾ 雪佛龙 + 标题
  `Terminal · <folder>`，点击切换 `termOpen`；展开时右侧追加 Refresh / Kill
  按钮），面板体（no-folder 提示 / Start 按钮 / snapshot 滚动区 + command… 输入
  + Send）在 `termOpen=true` 时显示。面板**跨会话标签共享**（不是某个 tab 的文档）。
  移除了原 `dt-terminal` 主区切换（`s.detailTab` 字段连带删除）；面板仍接
  `terminal_spawn` / `terminal_snapshot` / `terminal_write` / `terminal_kill`，
  维持 PTY 进程模型与缓存契约不变。渲染到 `src/app/render-out/terminal-panel.png`
  （展开）+ `terminal-panel-collapsed.png`（折叠）。
  **当时的最小切片**（多 tab + 上下文已于 2026-10-20 补齐，见下一条）：一个文件夹维持一个 PTY id。
- [x] **project-boot / import-sessions 实体化**（本次）：
  后端 `project_boot_*` 与 `scan_importable_sessions` / `import_selected_sessions` /
  `import_local_conversations` 早已就绪，面板从「占位薄壳」升级为真面板。
  - **import-sessions** (`src/app/import_sessions.go`)：标题 + 刷新；scan 摘要
    `Found N importable sessions across M folders`；每个 folder 一张卡片（名称 + agent 类型 + 路径 + Select all / Import all in folder）；每条 session 一行（**点击整行切换勾选**——避开 mygo `Checkbox` 需 `*bool` 但 map 值不可寻址的限制，用 `iconCheck`/`iconCircle` 复刻勾选态），状态徽章 New(绿)/ Imported(灰)/ Deleted(红)；底部 `Import selected` + 已选数；导入后显示结果摘要 `imported X · updated Y · restored Z · skipped W`（成功）或红色错误横幅（后端 `apiError`），刷新按钮清空勾选重扫。
  - **project-boot** (`src/app/project_boot.go`)：项目类型二选一（Shadcn / HyperFrames，按下高亮）；shadcn 表单 `Project name` / `Template` / `Preset code` / `Package manager`（npm/pnpm/yarn/bun Select）/ `Target directory`（当前路径 + `Choose…` 调 `hostOpenFiles` 钩子）；hyperframes 表单 `Project name` / `Example` / `Resolution` / `Package manager` / `Target directory`；name 空时按钮 disabled 并内联红字 `Project name is required`；点 Create 调 `create_shadcn_project` / `create_hyperframes_project`，成功显示 `Project created at <path>`，失败显示后端错误。
  - **HyperFrames 技能**：独立卡片 `Detect HyperFrames skills`（`detect_hyperframes_skills`）——六个 agent 各一行，整行点击切换勾选 + `installed` / `not installed` 状态字；底部 `Install skills` 调 `install_hyperframes_skills(agents)`。
  - **i18n**：en + zh-Hans 加 22 条 `import*` / `pb*` 键（含 `@importSummary{importable,folders}` / `@importInFolder{path}` / `@pbCreated{path}` 三个 ICU 复合格式元数据），`mygo generate` 再生 `catalog_gen.go` 已提交。
  - **测试**：`src/app/panels_test.go` 替换原 `placeholder_routes_test.go`（路由已非占位）——`TestImportSessionsRenders` / `TestImportSessionsEmpty` / `TestImportSelectAllSelects` / `TestImportSelectedImports`（真后端 e2e，等 `imported` 出现）/ `TestProjectBootRenders` / `TestProjectBootSwitchToHyperframes` / `TestProjectBootCreateRequiresTemplate`（驱动后端 `ErrInvalidInput("Template is required")` 经错误横幅浮现）；`render_test.go` 加 `TestZZRenderImportSessions` / `TestZZRenderProjectBoot`（含 shadcn + hyperframes 两态）出 `import-sessions.png` / `project-boot.png` / `project-boot-hyperframes.png`。
  - **最小切片**：未做 shadcn 模板/preset 的预设下拉（自由输入 + 占位提示就够，CLI 端会校验真实模板名）、`hostOpenFiles` 在无宿主环境（headless / 测试）下 Choose… 静默无效、HyperFrames skill 实际的 skill-dir 枚举（后端现 hardcoded `installed: false`，故 UI 也只能显示 not installed）。
- [x] **命令运行器 command runner**（本次）：workbench 路由 `commands`（状态栏右侧
  `Commands` 入口，与 Import sessions / Project boot 并列），面板 `src/app/folder_commands.go`。
  - **folder 作用域**：无选中文件夹时提示 `Select a folder to manage its commands.`；
    有文件夹时按 `folderId` 读 `list_folder_commands`（缓存键 `folder-commands:<id>`）。
  - **列表**：每条命令一行——`name`（粗体）+ `command`（muted）右侧 `Run` / `Edit` / `Delete`
    （`iconX`）。命令 id 取 `intOf(m["id"])`。
  - **新增**：`New command` 表单（Name / Command 两个 `TextInput`）+ `Create`
    （任一为空时 Disabled）→ `create_folder_command`。
  - **编辑**：点 `Edit` 就地换成编辑行（Name / Command 输入 + `Save` / `Cancel`）→
    `update_folder_command`（id + name + command）。
  - **删除**：`delete_folder_command`（id）。
  - **从 package.json 导入**：`Bootstrap from package.json` →
    `bootstrap_folder_commands_from_package_json`（folderId + folderPath），成功后绿字
    `Imported N scripts from package.json.`（后端按 lockfile 选 npm/pnpm/yarn/bun 并把
    `scripts` 映射成 `<pm> run <name>`）。
  - **运行**：`Run` 把命令**送进底部终端面板**——`terminal_spawn`（workingDir=folder 路径）
    拿 id 后 `terminal_write` 写 `command + "\r"`，并置 `termOpen=true` 让面板展开可见。
  - **i18n**：en + zh-Hans 加 17 条 `commands*` + `routeCommands`（含
    `@commandsBootstrapped{count}` 的 int 占位 meta），`mygo generate` 再生 catalog。
  - **测试**：`src/app/commands_test.go`——`TestCommandsNoFolder` / `TestCommandsRendersEmpty` /
    `TestCommandsCreate`（真后端，settle 初始拉取后建命令等 `lint` 出现）/ `TestCommandsDelete`
    （建后删、等行消失）/ `TestCommandsBootstrap`（写真实 package.json，等 `Imported 3 scripts`）；
    `render_test.go` 加 `TestZZRenderCommands` 出 `commands.png`。
  - **坑**：`asListVal()` 只吃**已解码**的 `any`，对 `ensure()` 返回的 `json.RawMessage`
    永远返回 nil——必须用 `asList(raw)`（`json.Unmarshal`）。最初用错导致列表恒空、
    「命令建好却不显示」。
  - **最小切片**：`reorder_folder_commands` 未接拖拽/上下移 UI（列表按 `sort_order` 只读排序，
    新建自然追加）；命令无独立图标；`Run` 复用单一 `termID`（多命令并发跑会覆盖同一个 PTY）。
- [x] **remote-manage 远程管理页**（本次）：git 路由 `git-remotes`（`src/app/git_remotes.go`），
  入口是 aux Changes/Commits 头的 «Git 操作» 菜单里的 `Remotes`。
  - 列表：`git_list_remotes` → 每行 `name`（粗体）+ `url`（muted）+ `Fetch` / `Set URL` / `Remove`。
  - 新增：`Add remote` 表单（Name / URL + `Add`，任一空 Disabled）→ `git_add_remote`。
  - 改 URL：点 `Set URL` 就地换成编辑行（URL 输入 + `Save` / `Cancel`）→ `git_set_remote_url`。
  - 移除：`Remove` 两步确认（先翻成 `Confirm remove` / `Cancel`）→ `git_remove_remote`。
  - 拉取：`Fetch` → `git_fetch_remote`（单个 remote）。
  - 变更成功后同时失效 `gitremotes:<root>`、`pushinfo:<root>:` / `pushlog:<root>:` 前缀与
    `gitlog:<root>`（Push 页的 remote 选择器与未推送列表随之刷新）。
  - **最小切片**：未做 credential 输入（走后端 `credentialEnv` 的宿主凭据）；未接 `git_fetch_remote` 的
    进度/结果输出展示（只报「fetched」）；remote branch 的增删（`git_delete_remote_branch`）未进本页。
- [x] **clone 仓库对话框**（本次）：workbench 路由 `clone`（`src/app/clone.go`），入口是侧栏
  Folders 分段的 `Clone repo` 按钮（与 `Open folder` 并列）。
  - 表单：`Repository URL` + `Target directory`（+ `Choose…` 调 `hostOpenFiles` 宿主钩子）+ `Clone`
    （任一空 Disabled）→ `clone_repository`（后端 `git clone`，带 typed 错误：already_exists / not_found /
    network / auth_failed / permission_denied / external_command）。
  - 成功：绿字 `Cloned to <path>` + `Open folder` 按钮 → `add_folder_to_history` 并把该目录设为当前文件夹
    （清 `folders` 缓存、退出 clone 路由回工作台）。
  - **最小切片**：无凭据/分支/深度（depth）选项；clone 是同步阻塞的后端调用（无进度条），大仓库会让
    「Clone」按钮在请求期间无反馈。
- [x] **token-usage 工作台**（本次）：workbench 路由 `token-usage`（`src/app/token_usage.go`），
  入口是左侧导航 `Usage`（Automations / To-dos / Repositories / Canvas 之后）。
  - **汇总**：`token_usage_report`（filter: bucket + tzOffsetMinutes）→ 一行大数字
    `Total / Input / Output / Cache write / Cache read / Turns / Conversations / Active days`
    （`humanTokens` 压成 1.2k / 3.4M / 5.6B）。
  - **连续天数**：`streak` → `Streak · <current> days now · <longest> longest`。
  - **时间桶**：Day / Week / Month 三选一（按下高亮），切桶即换 `usage-report:<bucket>` 缓存键重取。
  - **活跃度**：`series` 取最近 14 桶画横向比例条（bucket_key + 条 + 值）。
  - **分维汇总**：`by_folder` / `by_agent` / `by_model` 各取前 6 画比例条。
  - **Top 会话**：`top_conversations`（标题 + agent + token），标题由后端 join 回填。
  - **工作时段热力**：`heatmap` → 7 行（周一→周日）× 24 列小方块，颜色按 `total_tokens`
    相对最大值调 alpha（`ui.Box().Background(text.Alpha(...))`）。
  - **同步**：`Sync` → `token_usage_sync`（后端当前是 no-op，只报零），成功后失效
    `usage-status/facets/report:{day,week,month}` 并显示 `sync complete`。
  - **空态/截断**：total_tokens=0 → `No usage recorded yet. Sync to populate the dashboard.`；
    `truncated` → 截断提示行。
  - **i18n**：en + zh-Hans 加 39 条 `usage*` + `navUsage`（含 `@usageStreakValue{current,longest}` /
    `@usageStatus{fact,synced,total}` 两个 int 占位 meta，`@usageHeatmapPeak{hour}` /
    `@usageHeatmapCell{weekday,hour,value}` 两个悬停提示 meta）。
  - **测试**：`token_usage_test.go` 9 个（Empty / Renders 断言 12 处 / BucketSwitch / Sync 真后端 /
    RangeSwitch / Compare / MultiSelectFilters / FilterBodyArrays / HeatmapTooltip）；
    `render_test.go` 加 `TestZZRenderTokenUsage` 出 `token-usage.png`（打开 Folder 多选清单）。
  - **坑（同类第二次）**：`asObjVal()` / `asListVal()` 只吃**已解码**的 `any`；对 `ensure()` 返回的
    `json.RawMessage` 恒返回 nil。顶层 report/facets/status 必须用 `asObj(raw)` / `asList(raw)`；
    只有从已解码 map 里再取子对象（`report["totals"]`）才用 `asObjVal`。
  - **筛选器（2026-10-20 深化）**：`Range` 预设 `All / 7d / 30d / 90d`（客户端算 start/end RFC3339）
    + `Folder` / `Agent` / `Model` 三个**多选**（上游 MultiSelectFilter 的同形实现：按钮 +
    `ui.Popover` + 原生 `ui.Checkbox` 清单；清单保持打开，可连续勾选），按钮标题显示
    `Folder: proj +1`，任一维有选择时出现 `Reset filters`。
    选项来自 `token_usage_facets`；folder 以 id 存储（勾选值 → `folderIds` 数字数组），
    agent/model 直接以字符串数组发送；空维度不发送键（后端视为全部）。
    + `Compare previous` 开关（`comparePrevious:true`，需有界区间才有效）→ 显示
    `vs previous: +100.0%` 的环比。缓存键把 bucket/range/三组多选/compare 全部折入
    （`usageReportKey`），改任一维即换键重取。
  - **热力图悬停（2026-10-20）**：每个格子挂 `Element.Tooltip`（mygo 原生 tooltip，600ms 悬停 /
    键盘聚焦触发）→ `Mon 09:00 — 500 tokens`；补齐上游的星期栏（Mon..Sun）、稀疏小时刻度
    （00/06/12/18）、`Less…More` 图例，以及 `Tokens by local weekday and hour. Densest around 09:00.`
    提示行（`usagePeakHour` 复刻上游 peakHour）。
  - **最小切片**：sync 是后端 no-op；多选清单无搜索框（上游有 search/filter；当前 facets 数量有限）。
- [x] **settings → model-providers 页**（本次）：`src/app/model_providers.go`，settings 子导航
  新增 `Model providers`（Agents 之后），接 `list/create/update/delete_model_provider`
  （纯存储 CRUD，不碰上游的 provider 连接逻辑）。
  - **列表**：每行 `name` + `agent_type` + `model` + `api_url` + `API key: <masked>`
    （后端 `api_key_masked`，只留首尾 4 字符）；右侧 `Edit` / `×`（两步确认 `Confirm delete`）。
  - **新增**：`Add provider` 表单（Name / API URL / API key / Agent type / Model，Name+URL 空则
    Create 禁用）→ `create_model_provider`。
  - **编辑**：`Edit` 就地换编辑行（五个输入 + Save/Cancel）→ `update_model_provider`；
    **api key 留空 = 保留原值**（body 不带 `apiKey`，后端就不会覆盖）。
  - **删除** → `delete_model_provider`。
  - **i18n**：en + zh-Hans 加 20 条 `mp*` + `navModelProviders`（含 `@mpDeletePrompt{name}` meta）。
  - **测试**：`model_providers_test.go` 4 个（Empty / Create / Edit 改名 / Delete 两步确认，
    全走真后端 CRUD）；`render_test.go` 加 `TestZZRenderModelProviders` 出 `model-providers.png`。
  - **最小切片**：未做「测试连接 / 拉取模型列表」（上游 provider 逻辑明确不复刻）；
    agent type 是自由文本而非从 `acp_list_agents` 拉的下拉；无排序/启用开关。
- [x] **browser 面板**（本次）：workbench 路由 `browser`（`src/app/browser_panel.go`），
  入口是左侧导航 `Browser`（Usage 之后）。
  - **地址栏**：`Address` 输入 + `Open in browser`（`c.OpenURL` 拉起系统浏览器，
    空地址时禁用）+ `Open via bridge`（`browser_bridge_open`）。
  - **桥接状态**：`browser_bridge_status` → running / stopped；ports / public_host；
    未启用时显示诚实提示——本构建不跑端口桥接，面板无法内嵌页面，请用「Open in browser」。
  - **入口**：`Open Settings → Browser` 直接跳到 settings 的 browser 页。
  - **i18n**：en + zh-Hans 加 11 条 `browser*`（复用 `navBrowser`）。
  - **测试**：`browser_panel_test.go` 3 个（Renders / OpenBridge 报 `config_missing` /
    OpenDisabledWithoutURL）；`render_test.go` 加 `TestZZRenderBrowserPanel` 出 `browser-panel.png`。
  - **降级说明（重要）**：上游是 webview 内嵌 loopback dev server；**mygo 无 webview**，
    且 Go 后端 `browser_bridge_*` 明确是 no-op（status enabled:false / open config_missing）。
    因此本面板是「地址栏 + 外部打开 + 桥接状态 + 设置入口」的诚实降级面，不是内嵌浏览器。
  - **最小切片**：无内嵌页面/前进后退/刷新当前页/端口桥接配置 UI（桥接子系统本身未移植）。
- [x] **workspace-folder / upload 对话框**（本次）：workbench 路由 `workspace-folders`
  （`src/app/workspace_folders.go`），入口是 aux **Files** tab 头的文件夹图标（`files-links`）。
  - **文件夹链接**：`list_folder_links` → 每行 `name` + `targetPath` + 状态
    （ok 绿 / missing·broken 橙 / conflicted 红，来自后端 `linkStatus`）+ `Rename` / `×`（两步确认）。
    status 为 missing/broken 时另有 `Recreate link` 图标 → `repair_folder_link`（后端按记录名重建
    junction/symlink）；成功后失效 `folder-links:<fid>` 并显示 `link recreated`。conflicted 不给修复
    （名字已被真实目录占用，按上游语义需人工处理）。
  - **新增链接（两步）**：`Link a folder`（Folder to link + `Choose…` 走 `hostOpenFiles`；Link name
    可选；target 空则 Add link 禁用）→ **dry-run** `preview_folder_links {folderId,paths:[path]}`：
    - 可用计划 → 「Ready to link」卡：可编辑名称输入（预填后端给的去重名）、
      `Keep links out of git status` 开关（`wfGitExclude`，默认开）、`Discard` /
      `Link 1 folder(s)`；确认后才 `create_folder_links`（`items:[{path,name}]` + 开关值），
      成功后失效 `folder-links:<fid>` 与 `ft:<root>`。
    - 改名提示：`renamed` 时按 `collidesWithExistingEntry` 显示
      “Renamed — {base} already exists in this folder” 或 “… is already used by another link”；
      否则显示 “Will appear as {name}”。
    - 拒绝 → 琥珀色警告行：本地化 `not_found` / `not_a_directory` / `same_as_root` /
      `ancestor_of_root` / `inside_root` / `already_linked`（有 `existingLinkName` 时为
      `Already linked as {name}`）/ `name_unavailable`；`Cancel` 关闭，不建任何链接。
  - **重命名**：`Rename` 就地编辑 → `rename_folder_link`（`linkId` + `newName`）。
  - **移除** → `remove_folder_link`（`linkId`，后端删链接目录 + DB 行）。
  - **上传**：`Upload a file`（`Choose file…` 走 `hostOpenFiles` + Target subpath 可选 + `Upload`）→
    **multipart** `upload_workspace_file`（`root_path` / `relative_path` / `file`）→ 绿字
    `uploaded <path>`；成功后失效 `ft:<root>`。为此给 `Backend` 加了 `upload()` 方法
    （`Backend.raw` 只发 JSON，upload 路由要 `multipart/form-data`，且 `root_path` 字段须在 file 之前）。
  - **i18n**：en + zh-Hans 共 47 条 `wf*`（含 `@wfRemovePrompt{name}` / `@wfUploaded{path}` /
    `@wfWillAppearAs{name}` / `@wfRenamedForDuplicate{base}` / `@wfRenamedForExistingEntry{base}` /
    `@wfLinkCount{count}` / `@wfRejectAlreadyLinkedAs{name}` meta）。
  - **测试**：`workspace_folders_test.go` 8 个（Renders / CreateLink 预览后确认并真建 junction /
    PreviewRejects 根目录拒绝 + Cancel / PreviewAlreadyLinked / PreviewRenames 占名后 `-2` 落盘 /
    RepairLink 删链接项后重建 / RemoveLink 两步确认 / UploadFile 真 multipart 并验证落盘）；
    `render_test.go` 的 `TestZZRenderWorkspaceFolders` 快照改为「两条链接 + 待确认预览卡」，
    出 `workspace-folders.png`。
  - **最小切片**：UI 一次预览一个路径（后端 `paths` 支持批量；上游多选树 + pending 列表未做），
    名称不在客户端校验（后端去重为 `base-2..-99`）；上传只支持单文件、无进度条、无多选。
- [x] **settings → agents 深化**（本次）：`src/app/settings_agents.go`（从 settings.go 拆出），
  内置目录 + 按需版本探测 + 自定义 agent 注册。
  - **内置目录**：`acp_list_agents` → 每行 `name` + `agent_type` + 徽标（已探测 `vX.Y.Z` /
    `● available` / `○ not installed`）+ `Check`。`Check` 单飞（`agentChecking`）：
    探测期间所有按钮禁用并显示 `Checking…`；调 `acp_detect_agent_local_version` +
    `acp_fetch_agent_latest_release`，结果进 `agentVer` / `agentLatest`；无本地安装显示
    `No local installation found.`，失败显示红色 `Version check failed: …`，成功 toast
    `<name> checked`。
  - **帮助文案**：解释 ACP 启动与 “available” 的含义（启动器能否在本机解析），避免把
    available 误读成「已安装版本」。
  - **自定义 agent**：`acp_list_custom_agents` → 行（name / `custom:<id>` / 分发方式 / 版本 /
    `Delete` 两步确认）；`launchable=false` 时版本橙色并显示 `Cannot launch: {problem}`。
    `Add custom agent` 打开表单：Registry id / Name / Description / Distribution（原生
    `ui.Select` npx|uvx）/ Package / Version / Skills directory。
  - **校验**：镜像后端规则 —— id 必填、≤64、不以 `.` 开头、仅 `[A-Za-z0-9._-]`；name 必填；
    package 必填；version 可空但非空时 ≤128 同字符集；skills dir 必须绝对路径或 `~/…`。
  - **保存/删除**：`acp_save_custom_agent {params:{registry_id,name,description,version,
    distribution_kind,spec:{npx|uvx:{package}},skills_dir?}}`；`acp_delete_custom_agent {registryId}`。
    成功 toast 并失效 `custom-agents`；失败显示在表单错误行 / 删除 toast。
  - **i18n**：en + zh-Hans 加 32 条 `setAgent*` / `setCustomAgent*` / `setAddCustomAgent` /
    `setChecking` / `setSaveCustomAgent`（含 `@setAgentCheckFailed` / `@setAgentChecked` /
    `@setCustomAgentSaved` / `@setCustomAgentDeleteFailed` / `@setCustomAgentDeletePrompt` /
    `@setCustomAgentProblem` meta）。
  - **测试**：`settings_agents_test.go` 4 个（RendersAndChecks 断言四种检查结果 + 自定义空态 /
    CheckVersionFeedback 假 agent 只走本地探测 / CustomCreateAndDelete 逐条校验后真后端建删 /
    CustomAgentRules 纯函数表）；快照 `settings-agents.png` / `settings-agents-form.png` /
    `settings-agents-custom.png`。
  - **最小切片**：自定义 agent 只能新增/删除（无编辑；同名保存即覆盖）、registry catalog
    一键添加未接（后端同样未移植）、表单错误汇总在底部一行（无逐字段高亮）。
- [x] **settings → collaboration 页**（本次）：`src/app/settings_collaboration.go`，接
  `get/set_delegation_settings` 与六组对话内工具开关。
  - **Delegation**：原生 `ui.Switch` 总开关 + 原生 `ui.StepSlider(1..8)` 深度 + 缓存 MB 输入；
    `General` / `Agent defaults` 用原生 `ui.Tabs` 切换；agent defaults 每行
    `Mode: … · N config`，`Remove override` 拷贝式删键；`Save` 仅在 dirty 时可用
    （`reflect.DeepEqual` 基线），成功后重新拉取服务器回填（深度由后端 clamp）。
  - **对话内工具**：feedback / question / session_info / browser(+eval) / computer /
    automations / work_tasks 八个原生开关；browser 关闭时 eval 强制 false 且禁用；
    Save 只写变更组（`set_feedback_settings` / `set_question_settings` /
    `set_session_info_settings` / `set_browser_tools_settings` / `set_computer_tools_enabled` /
    `set_chat_authoring_settings`）。
  - **顺带修后端**：`get/set_session_info_settings` 默认值对齐上游为 **ON**
    （`src-go/web/handlers/settings_misc.go`，测试同步）。
  - **i18n**：en + zh-Hans 加 `setCollab*` / `setTool*` / `setTools*` / `setUnsaved`
    （含 `@setCollabDefaultsMode` / `@setCollabLoadFailed` meta）；删掉三个占位 hint。
  - **测试**：`settings_collaboration_test.go` 2 个（Renders + 真后端 save/reseed；中文 chrome）；
    `dumpFrame` 出 `settings-collaboration.png`。
- [x] **settings → shortcuts 页**（本次）：`src/app/settings_shortcuts.go`，原生按钮录键。
  - **绑定表**：24 个已接线动作，顺序与上游 `SHORTCUT_DEFINITIONS` 一致（search / sidebar /
    terminal / new terminal / close terminal / aux / new conversation / open folder / settings /
    close tab / reopen / next / prev / tab 1..9 / send / newline）；默认弦、标题与说明对齐上游；
    macOS 显示 ⌘⌥⇧，其它平台 Ctrl+Alt+Shift+。
  - **共享弦**：实现 `SHARED_SHORTCUT_PAIRS`——new terminal ↔ new conversation（⌘T）与
    close terminal ↔ close tab（⌘W）。终端面板打开且指针/输入行在其内时归终端，否则归会话；
    二者同弦时只注册一个窗口动作再按区域分派（mygo 一条弦只投递给一个动作）。
  - **取消绑定**：每行 × 把动作持久化为空弦；空弦真正不触发，reload 后仍是
    `Unassigned`，不回落默认。
  - **录键**：点击右侧按钮进入录制；mygo 的 `OnShortcut` 是精确匹配，因此录制帧把
    Cmd / Cmd+Shift / Cmd+Alt / Cmd+Shift+Alt × 可录键全部注册为捕获，Esc 取消；
    send / newline 额外接受无修饰键；冲突时保留录制并 toast `Shortcut is already used by "…"`。
  - **持久化**：`~/.gocode/shortcuts.json`（尊重 `GOCODE_DATA_DIR`，0600），
    启动时以默认值为底、只覆盖合法弦；`Reset defaults` 重写文件。
  - **接线**：命令面板、左右侧栏 / aux、底部终端开合、终端标签新建/关闭、新建会话、
    打开文件夹 / 设置、关闭当前标签页、reopen 原位恢复、标签页前后 / 数字跳转；composer
    的 send / newline 走 `TextArea.OnTextKey`（含自定义弦）。
  - **测试**：`settings_shortcuts_test.go` 8 个（Record/Persist/Conflict/Escape/Reset +
    重新加载；共享弦；终端/会话路由；reopen；composer 发送/换行；chrome 开关；标签导航）；
    `TestMain` 把测试偏好隔离到临时目录；`dumpFrame` 出 `settings-shortcuts.png`。
  - **最小切片**：上游的 close_all_file_tabs（无文件标签）、toggle_custom_style（无自定义
    样式子系统）、zoom_in/out/reset（原生窗口无全局缩放）本 build 未移植，因此未列出。
- [x] **settings → skill-packs 页**（本次）：`src/app/settings_skill_packs.go`。
  - **原生 `ui.Tabs`**：Experts / Science / Office Tools / Custom 四页复用既有正文
    （`expertsBody` / `scienceBody` / `officeToolsBody` / `customSkillsBody`），不再各自带页头。
  - **共享控件**：`Open central folder`（`experts_open_central_dir` → 下一帧交给
    `c.OpenURL("file://…")`）、`Refresh`（同时失效 experts / science / office / custom 缓存）。
  - **i18n**：en + zh-Hans 加 `setSkillPack*`（含 `@setSkillPacksOpenFailed` meta）。
  - **测试**：`settings_skill_packs_test.go` 2 个（四 tab 正文 / 中文 chrome）；
    `dumpFrame` 出 `settings-skill-packs.png`。
  - **最小切片**：open-folder 走宿主 URL opener（mygo 无 reveal API），headless 测试不点击它。
- [x] **terminal 多标签 + worktree 上下文**（本次，2026-10-20）：`src/app/terminal.go`
  对照上游 `terminal-panel.tsx` / `terminal-tab-bar.tsx` / `terminal-context.tsx`。
  - **标签条**：展开时顶栏是 32px 标签条——标签（标题 + 关闭）、新建、刷新、折叠；
    收起时退回单行「Terminal · <path>」并可点开。标签过多时横向滚动。
  - **每 tab 独立 PTY**：tab id 直接作为 `terminalId` 传给 `terminal_spawn`，关闭/Close Others/
    Close All 逐个 `terminal_kill`；某 tab 启动失败只在该 tab 内显示错误，不吞掉标签。
  - **目录上下文**：`termTab` 保存创建时的 `FolderID` + `WorkingDir`；从 worktree 文件夹建的
    终端始终跑在该 worktree，切换会话/文件夹不改变已有标签的 cwd。命令面板的 Run 也改为
    新建标签并以 `initialCommand` 启动（上游 `createTerminalWithCommand`），标题取命令名。
  - **右键菜单**：Rename（内联输入 + 保存/取消）、Close、Close Others、Close All。
  - **firehose**：按 `terminal://…/{id}` 的 id 失效对应快照，后台标签的输出也会在切回时刷新。
  - **测试**：`terminal_test.go` 10 个（切换/关闭回落到邻位、Rename、Close Others/All、
    创建目录不随选择漂移、zh chrome）；`render_test.go` 的 `terminal-panel.png` 改为双标签。
  - **真终端模拟器（2026-11-04，替换文本回放）**：面板体换成 mygo 0.7 的 terminal 插件
    （libghostty-vt）——光标/ANSI 全彩/选区复制/回滚重排/全屏程序，架构同上游（模拟器在 UI、
    PTY 在后端）：`ptyConn`（`terminal.Options.Conn`）把 firehose 推送的 `TerminalEvent` VT 块
    喂给模拟器（spawn 后先 `terminal_snapshot` priming，seq 去重防重），键击经写队列走
    `terminal_write`，视图尺寸合并 latest-wins 走 `terminal_resize`，`terminal://exit` 以 EOF
    收尾（视图显示 [Process exited]）。输入行/Send/快照回放/Refresh 退役（模拟器自己收键；
    `TerminalPlaceholder/Send/Refresh/Exited` 四键删除）；OSC 标题随 tab 标题（用户改名后不覆盖）。
    输出行断言经 `term.Text()`（模拟器格子不是 ui.Text，`tt.Texts` 看不见）。
    顺带修**存量后端 bug**：裸 shell 名（`powershell.exe`）+ `Cmd.Dir` 在 Windows 被 exec 的
    CreateProcess 硬化解析进 workingDir → spawn 必败（旧 handlers 测试因此静默 skip）——
    spawn 前经 `exec.LookPath` 解析为绝对路径。

> **宏观结构对齐清单（2026-10-17 起）至此全部勾选完成。** 上游的主面板 / 路由 / 工作台
> 在本 build 中均已有对应物（部分为最小切片或诚实降级面，已在各条注明）。后续若继续，
> 属于「深化既有面板」而非「补宏观结构」。

> 注：本文档只列 UI（前端）缺口；后端路由/契约层的剩余项见 `docs/ROUTE_INVENTORY.md`。

# Addon 子进程异常退出后的单次恢复与关闭回收

> **Workspace:** F:\swe\098901\project-01

---

**User**

使用 forge 或日志 addon 的管理员会发现，addon 子进程一旦异常退出，登录、仓库同步、流水线状态回写或日志读写会持续返回 RPC 错误，Woodpecker 退出后还可能残留由它启动的进程。修复这条生命周期链路，使服务端始终保有对 forge 与日志 addon 进程的明确所有权：检测到当前 RPC 连接因子进程退出而失效时，本次调用应返回原错误，不得自动重放可能已经执行的写操作；后续调用可以启动新的 addon 并恢复服务，同一 addon 上并发到达的请求只能共同触发一次有界重建，启动持续失败时应及时返回且允许以后的请求再次尝试。服务器开始关闭后必须阻止任何新进程启动，等待或终止正在切换的实例，并回收当前以及运行期间替换过的所有 addon 子进程；未配置 addon 的内置 forge、数据库日志和文件日志路径保持现有行为。

**Assistant**

toolName: view_folder

status: success

f:\swe\098901\project-01

这是 Woodpecker CI 的代码库。关键目录是 forge addon 和 log addon。让我先阅读这些核心文件。

toolName: view_files

status: success

filePath: f:\swe\098901\project-01\server\forge\addon\plugin.go

toolName: view_files

status: success

filePath: f:\swe\098901\project-01\server\forge\addon\client.go

toolName: view_files

status: success

filePath: f:\swe\098901\project-01\server\forge\addon\server.go

toolName: view_files

status: success

filePath: f:\swe\098901\project-01\server\forge\addon\args.go

现在看日志 addon 的实现：

toolName: view_files

status: success

filePath: f:\swe\098901\project-01\server\services\log\addon\client.go

toolName: view_files

status: success

filePath: f:\swe\098901\project-01\server\services\log\addon\plugin.go

toolName: view_files

status: success

filePath: f:\swe\098901\project-01\server\services\log\addon\server.go

现在查看 addon 的 Load 在哪里被调用，以及服务设置：

toolName: view_files

status: success

filePath: f:\swe\098901\project-01\server\forge\setup\setup.go

toolName: view_files

status: success

filePath: f:\swe\098901\project-01\server\services\setup.go

toolName: view_files

status: success

filePath: f:\swe\098901\project-01\server\services\log\service.go

让我看看服务器启动时 forge 和 log 服务如何被加载，以及 manager.go：

toolName: view_files

status: success

filePath: f:\swe\098901\project-01\cmd\server\setup.go

toolName: view_files

status: success

filePath: f:\swe\098901\project-01\server\services\manager.go

现在查看 go-plugin 库版本及其 API（进程退出检测、错误类型）：

toolName: search_by_regex

status: success

toolName: view_files

status: success

filePath: f:\swe\098901\project-01\shared\logger\addon_logger.go

查看 go-plugin 库的客户端 API（模块缓存中）：

toolName: Shell

status: success

toolName: search_by_regex

status: success

toolName: view_files

status: success

filePath: C:\Users\czxy4\go\pkg\mod\github.com\hashicorp\go-plugin@v1.8.0\client.go

toolName: view_files

status: success

filePath: C:\Users\czxy4\go\pkg\mod\github.com\hashicorp\go-plugin@v1.8.0\client.go

toolName: view_files

status: success

filePath: f:\swe\098901\project-01\cmd\server\server.go

让我查看 go-plugin 的 Start 超时常量和启动失败处理逻辑：

toolName: view_files

status: success

filePath: C:\Users\czxy4\go\pkg\mod\github.com\hashicorp\go-plugin@v1.8.0\client.go

toolName: view_files

status: success

filePath: C:\Users\czxy4\go\pkg\mod\github.com\hashicorp\go-plugin@v1.8.0\rpc_client.go

查看 go.mod 的模块路径与 Go 版本，以及 ttlcache 用法和 forge.Forge 接口：

toolName: view_files

status: success

filePath: f:\swe\098901\project-01\go.mod

toolName: view_files

status: success

filePath: f:\swe\098901\project-01\server\forge\forge.go

现在搜索所有引用 addon.Load 的地方，以及检查现有测试约定：

toolName: search_by_regex

status: success

toolName: search_by_regex

status: success

toolName: view_files

status: success

filePath: f:\swe\098901\project-01\docs\docs\92-development\100-addons.md

toolName: view_files

status: success

filePath: f:\swe\098901\project-01\server\forge\addon\args_test.go

我已经完整理解了这条生命周期链路的问题。让我建立任务清单并开始实施修复。

toolName: todo_write

status: success

Todos updated: 6 items

## 1. 创建共享的 addon 生命周期管理包

toolName: Write

status: success

filePath: f:\swe\098901\project-01\shared\addon\manager.go

changes: undefined

## 2. 编写 Manager 集成测试（自举真实 go-plugin 子进程）

toolName: Write

status: success

filePath: f:\swe\098901\project-01\shared\addon\manager_test.go

changes: undefined

修正测试文件中的几处笔误（移除不存在的调用，使用标准 context 包）：

toolName: edit_file_search_replace

status: success

filePath: f:\swe\098901\project-01\shared\addon\manager_test.go

changes: undefined

toolName: edit_file_search_replace

status: success

filePath: f:\swe\098901\project-01\shared\addon\manager_test.go

changes: undefined

toolName: edit_file_search_replace

status: success

filePath: f:\swe\098901\project-01\shared\addon\manager_test.go

changes: undefined

toolName: edit_file_search_replace

status: success

filePath: f:\swe\098901\project-01\shared\addon\manager_test.go

changes: undefined

toolName: edit_file_search_replace

status: success

filePath: f:\swe\098901\project-01\shared\addon\manager_test.go

changes: undefined

toolName: edit_file_search_replace

status: success

filePath: f:\swe\098901\project-01\shared\addon\manager_test.go

changes: undefined

让我确认 go-plugin 会为子进程注入环境变量（自举测试依赖这一点）：

toolName: search_by_regex

status: success

需要给测试子进程命令设置标记环境变量：

toolName: edit_file_search_replace

status: success

filePath: f:\swe\098901\project-01\shared\addon\manager_test.go

changes: undefined

先运行核心包测试验证设计：

toolName: Shell

status: success

变量名 `errors` 遮蔽了标准库包，重命名：

toolName: edit_file_search_replace

status: success

filePath: f:\swe\098901\project-01\shared\addon\manager.go

changes: undefined

toolName: Shell

status: success

编译通过。运行集成测试：

toolName: Shell

status: success

核心生命周期测试全部通过（真实子进程验证）。更新进度，改造 forge addon：

toolName: todo_write

status: success

Todos updated: 6 items

## 3. 改造 forge addon

toolName: Write

status: success

filePath: f:\swe\098901\project-01\server\forge\addon\plugin.go

changes: undefined

现在重写 forge addon 的 client.go：

toolName: Write

status: success

filePath: f:\swe\098901\project-01\server\forge\addon\client.go

changes: undefined

## 4. 改造 log addon

toolName: Write

status: success

filePath: f:\swe\098901\project-01\server\services\log\addon\plugin.go

changes: undefined

toolName: Write

status: success

filePath: f:\swe\098901\project-01\server\services\log\addon\client.go

changes: undefined

编译两个 addon 包：

toolName: Shell

status: success

局部变量 `rpc` 遮蔽了 `net/rpc` 包名，重命名为 `frontend`：

toolName: edit_file_search_replace

status: success

filePath: f:\swe\098901\project-01\server\forge\addon\client.go

changes: undefined

toolName: edit_file_search_replace

status: success

filePath: f:\swe\098901\project-01\server\forge\addon\client.go

changes: undefined

toolName: edit_file_search_replace

status: success

filePath: f:\swe\098901\project-01\server\forge\addon\client.go

changes: undefined

toolName: edit_file_search_replace

status: success

filePath: f:\swe\098901\project-01\server\services\log\addon\client.go

changes: undefined

toolName: edit_file_search_replace

status: success

filePath: f:\swe\098901\project-01\server\services\log\addon\client.go

changes: undefined

toolName: edit_file_search_replace

status: success

filePath: f:\swe\098901\project-01\server\services\log\addon\client.go

changes: undefined

toolName: Shell

status: success

## 5. 在服务器关闭路径接入 ShutdownAll

toolName: edit_file_search_replace

status: success

filePath: f:\swe\098901\project-01\cmd\server\server.go

changes: undefined

toolName: edit_file_search_replace

status: success

filePath: f:\swe\098901\project-01\cmd\server\server.go

changes: undefined

现在给两个 addon 包补充 Load 失败快速返回的测试：

toolName: Write

status: success

filePath: f:\swe\098901\project-01\server\forge\addon\client_test.go

changes: undefined

toolName: Write

status: success

filePath: f:\swe\098901\project-01\server\services\log\addon\client_test.go

changes: undefined

## 6. 全量构建与验证

toolName: todo_write

status: success

Todos updated: 6 items

toolName: Shell

status: success

web 嵌入错误与本次改动无关（前端 dist 未构建）。确认一下：

toolName: view_files

status: success

filePath: f:\swe\098901\project-01\web\web.go

toolName: view_files

status: success

filePath: f:\swe\098901\project-01\web\web_external.go

前端未构建是环境既有情况，可用 `external_web` 构建标签绕过 embed 来验证全部 Go 代码：

toolName: Shell

status: success

全部编译通过。运行 vet 和相关包测试：

toolName: Shell

status: success

toolName: Shell

status: success

运行周边相关测试套件确认无回归：

toolName: Shell

status: success

两个失败看起来与本次改动无关（bitbucket DC 的测试夹具 500、Windows 文件占用）。验证它们是否为既有失败——先确认这些包不依赖我改动的代码：

toolName: Shell

status: success

toolName: Shell

status: success

用 git stash 验证这两个失败在未改动的代码树上同样存在：

toolName: Shell

status: success

确认两个失败均为既有环境问题，与本次改动无关。继续运行其余受影响的测试包：

toolName: Shell

status: success

cmd/server 测试同样受 web/dist embed 限制，加标签运行：

toolName: Shell

status: success

这两个也是 Windows 平台语义差异（unix socket 权限、os.Stat 行为）。快速用 stash 确认其为既有失败：

toolName: Shell

status: success

确认均为既有失败。补跑之前输出被截断的 `server/services` 根包以及 store 相关测试：

toolName: Shell

status: success

对核心并发逻辑跑一次 `-race` 检测：

toolName: Shell

status: success

toolName: Shell

status: success

全部通过。最后统一跑 gofmt 检查格式并复查改动文件：

toolName: Shell

status: success

Go 方法不支持类型参数（本机 gofmt 比编译器严格），改为包级泛型函数，兼容性更好：

toolName: edit_file_search_replace

status: success

filePath: f:\swe\098901\project-01\server\forge\addon\client.go

changes: undefined

批量替换 forge client.go 中其余调用点：

toolName: edit_file_search_replace

status: success

filePath: f:\swe\098901\project-01\server\forge\addon\client.go

changes: undefined

对 log addon 做同样重构：

toolName: edit_file_search_replace

status: success

filePath: f:\swe\098901\project-01\server\services\log\addon\client.go

changes: undefined

toolName: edit_file_search_replace

status: success

filePath: f:\swe\098901\project-01\server\services\log\addon\client.go

changes: undefined

统一 gofmt 格式化并重新验证：

toolName: Shell

status: success

格式化干净、编译与 vet 通过。重构后重跑全部相关测试（含 race）：

toolName: Shell

status: success

全部通过。最后通读重构后的 forge client.go 关键段落确认替换无误：

toolName: view_files

status: success

filePath: f:\swe\098901\project-01\server\forge\addon\client.go

toolName: view_files

status: success

filePath: f:\swe\098901\project-01\server\services\log\addon\client.go

最终全量编译确认并查看改动总览：

toolName: Shell

status: success

toolName: todo_write

status: success

Todos updated: 6 items

快速核对仓库的 lint 配置，避免新代码触发特有规则：

toolName: search_by_regex

status: success

toolName: view_files

status: success

filePath: f:\swe\098901\project-01\.golangci.yaml

toolName: view_files

status: success

filePath: f:\swe\098901\project-01\.golangci.yaml

查看 depguard 对 shared 包的约束和 mnd 配置：

toolName: view_files

status: success

filePath: f:\swe\098901\project-01\.golangci.yaml

toolName: search_by_regex

status: success

toolName: view_files

status: success

filePath: f:\swe\098901\project-01\.golangci.yaml

mnd 已排除测试文件。用 gofumpt（仓库 formatter 之一）复核格式：

toolName: Shell

status: success

gofumpt 无问题。尝试运行 golangci-lint（若本机可用）：

toolName: Shell

status: success

toolName: Shell

status: success

修复 lint 问题。先看 importas 配置中已有的别名约定：

toolName: search_by_regex

status: success

toolName: view_files

status: success

filePath: f:\swe\098901\project-01\.golangci.yaml

按项目约定（如 `service_log`）将别名加入 importas 配置，统一为 `shared_addon`，并修复 godot 与 modernize：

toolName: edit_file_search_replace

status: success

filePath: f:\swe\098901\project-01\.golangci.yaml

changes: undefined

toolName: edit_file_search_replace

status: success

filePath: f:\swe\098901\project-01\shared\addon\manager.go

changes: undefined

toolName: edit_file_search_replace

status: success

filePath: f:\swe\098901\project-01\shared\addon\manager.go

changes: undefined

修复注释与 var 块之间的空行，并更新导入别名：

toolName: edit_file_search_replace

status: success

filePath: f:\swe\098901\project-01\shared\addon\manager.go

changes: undefined

toolName: edit_file_search_replace

status: success

filePath: f:\swe\098901\project-01\server\forge\addon\client.go

changes: undefined

toolName: edit_file_search_replace

status: success

filePath: f:\swe\098901\project-01\server\services\log\addon\client.go

changes: undefined

toolName: edit_file_search_replace

status: success

filePath: f:\swe\098901\project-01\server\forge\addon\client_test.go

changes: undefined

批量替换标识符引用，并修正 log addon 测试文件的别名：

toolName: edit_file_search_replace

status: success

filePath: f:\swe\098901\project-01\server\forge\addon\client.go

changes: undefined

toolName: edit_file_search_replace

status: success

filePath: f:\swe\098901\project-01\server\services\log\addon\client.go

changes: undefined

toolName: edit_file_search_replace

status: success

filePath: f:\swe\098901\project-01\server\services\log\addon\client_test.go

changes: undefined

toolName: edit_file_search_replace

status: success

filePath: f:\swe\098901\project-01\server\services\log\addon\client_test.go

changes: undefined

toolName: edit_file_search_replace

status: success

filePath: f:\swe\098901\project-01\server\forge\addon\client_test.go

changes: undefined

重新构建并 lint：

toolName: Shell

status: success

修复 contextcheck 与 godot。将 Manager 注册改为显式控制（Load 首次启动成功后才注册，失败路径不再需要 context）：

toolName: edit_file_search_replace

status: success

filePath: f:\swe\098901\project-01\shared\addon\manager.go

changes: undefined

更新两个 Load：未注册构造、首次启动成功后注册，去掉 context 依赖：

toolName: edit_file_search_replace

status: success

filePath: f:\swe\098901\project-01\server\forge\addon\client.go

changes: undefined

不需要 nolint，正确应用选项方案：

toolName: edit_file_search_replace

status: success

filePath: f:\swe\098901\project-01\server\forge\addon\client.go

changes: undefined

toolName: view_files

status: success

filePath: f:\swe\098901\project-01\server\forge\addon\client.go

toolName: edit_file_search_replace

status: success

filePath: f:\swe\098901\project-01\server\forge\addon\client.go

changes: undefined

同样改造 log addon 的 Load：

toolName: edit_file_search_replace

status: success

filePath: f:\swe\098901\project-01\server\services\log\addon\client.go

changes: undefined

移除两个文件中不再使用的 context 导入，并修复 server.go 的 contextcheck/godot：

toolName: edit_file_search_replace

status: success

filePath: f:\swe\098901\project-01\server\forge\addon\client.go

changes: undefined

toolName: edit_file_search_replace

status: success

filePath: f:\swe\098901\project-01\server\services\log\addon\client.go

changes: undefined

toolName: edit_file_search_replace

status: success

filePath: f:\swe\098901\project-01\cmd\server\server.go

changes: undefined

toolName: edit_file_search_replace

status: success

filePath: f:\swe\098901\project-01\cmd\server\server.go

changes: undefined

toolName: Shell

status: success

forge 方法签名仍使用 context.Context，恢复导入：

toolName: edit_file_search_replace

status: success

filePath: f:\swe\098901\project-01\server\forge\addon\client.go

changes: undefined

toolName: Shell

status: success

Lint 零问题。最后重跑全部相关测试确认注册逻辑改动无回归：

toolName: Shell

status: success

全量 vet 与受影响的 setup 测试最终确认：

toolName: Shell

status: success

全量 vet 的告警均在未改动的既有文件（pipeline/runtime、agent/runner 的 lostcancel、构建标签相关），与本次改动无关；改动包 golangci-lint 零问题。查看最终改动清单：

toolName: Shell

status: success

生命周期链路已修复完成。以下是改动总结：

## 核心设计：新增 addon 子进程生命周期管理器

新增 [shared/addon/manager.go](file:///f:/swe/098901/project-01/shared/addon/manager.go)，由一个 `Manager` 明确持有一个 addon 可执行文件的所有子进程世代，逐条满足需求：

| 需求 | 实现 |
|---|---|
| 子进程退出后，**本次调用返回原错误、不重放写操作** | RPC 调用失败时原样返回错误，仅通过 `ReportFailure` 判定（`rpc.ErrShutdown` 或 go-plugin `Exited()`）后丢弃该死连接；业务错误（如 forge 404）不动连接 |
| 后续调用可启动新 addon 恢复服务 | 连接被标记失效后，下一次 `Current()` 懒启动新子进程 |
| 并发请求只共同触发**一次有界重建** | 单个 `rebuildOp` + channel 单飞；go-plugin `StartTimeout` 与整体超时均为 30s（`DefaultStartTimeout`） |
| 启动持续失败快速返回、以后可再试 | 失败立即释放全部等待者并清空重建标记，下一次请求重新尝试；超时迟到的启动结果一律回收、永不发布 |
| 关闭后阻止新启动、等待/终止切换中实例、回收所有世代 | `Shutdown` 置关闭闸门（新请求得 `ErrShuttingDown`），立即杀当前实例，等待在途重建结束并杀掉切换实例，`killWg` 保证运行期间被替换的子进程全部 `Kill` 完成；`ShutdownAll` 并行回收全部已注册 Manager（含 forge TTL 缓存运行期替换出的旧 addon） |

## 两个 addon 的改造

- [server/forge/addon/client.go](file:///f:/swe/098901/project-01/server/forge/addon/client.go) 与 [plugin.go](file:///f:/swe/098901/project-01/server/forge/addon/plugin.go)：`Load` 返回稳定的 `*RPC` 包装器，内部持有 Manager；新增仅宿主侧使用的 `hostPlugin` 把每个子进程世代的 net/rpc 连接交给 Manager。登录、仓库同步、状态回写等全部方法经统一泛型 `callRPC` 发出。
- [server/services/log/addon/client.go](file:///f:/swe/098901/project-01/server/services/log/addon/client.go) 与 [plugin.go](file:///f:/swe/098901/project-01/server/services/log/addon/plugin.go)：同样改造，`LogFind/LogAppend/LogDelete/StepFinished` 的错误语义保持不变。
- 两处 `Load` 保留"启动即失败则服务器启动失败"的既有行为；Manager 仅在首次启动成功后注册，失败不留残余所有权。
- 内置 forge、数据库日志（默认 store）、文件日志路径完全不经过 Manager，行为不变。

## 服务器关闭接线

[cmd/server/server.go](file:///f:/swe/098901/project-01/cmd/server/server.go#L129-L140) 在服务初始化后注册 defer：退出时以独立于已取消 ctx 的 45s 预算调用 `addon.ShutdownAll`，在 store 关闭前完成全部回收。

## 验证

- 新增 [shared/addon/manager_test.go](file:///f:/swe/098901/project-01/shared/addon/manager_test.go)：自举真实 go-plugin 子进程，覆盖崩溃后原错误返回+下次重建、30 并发单飞（启动计数恰为 2）、失败快速返回且第三次成功、200ms 有界超时、两代子进程在 Shutdown 后均 `Exited()`、关闭后拒绝新启动、`ShutdownAll`——7 项全部通过，`-race` 干净。
- 另加两个 `Load` 缺失可执行文件快速失败测试；全仓库 `go build`、`golangci-lint`（改动范围 0 issues）、`gofumpt -extra` 通过；[.golangci.yaml](file:///f:/swe/098901/project-01/.golangci.yaml) 按既有约定登记 `shared_addon` 导入别名。
- `TestBitbucketDC`、`TestReadAgentIDFileExists`、`TestSetUnixSocketPermission`、`TestCheckSqliteFileExist` 四个失败经 stash 对照确认是该 Windows 环境的既有失败，与本次改动无关。

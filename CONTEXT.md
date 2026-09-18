# vulhub-cli

在一台机器上管理 vulhub 靶场生命周期的命令行工具。术语围绕两组概念展开：**靶场的身份**（怎么指代一个靶场）和**靶场的运行状态**（哪些正在跑）。

## Language

### 靶场的身份

**靶场 (Environment)**:
vulhub 仓库中一个含 `docker-compose.yml` 的目录，例如 `activemq/CVE-2023-46604`。它是一个静态定义，本身不含任何运行状态。
_Avoid_: 漏洞环境、靶机、环境

**靶场路径 (Environment Path)**:
靶场相对 vulhub 检出根目录的目录路径。它是靶场唯一的身份标识。
_Avoid_: 靶场名、靶场 ID

**靶场编号 (Environment Number)**:
分配给靶场的永久整数别名，让用户不必输入完整路径。一旦分配就不随 vulhub 更新而改变；靶场消失后该编号作废，不再复用。
_Avoid_: 序号、行号、ID、index

**索引 (Index)**:
靶场路径与靶场编号之间的持久映射。它是工具唯一需要持久保存的状态。
_Avoid_: 缓存、数据库、注册表

### 靶场的运行状态

**运行中靶场 (Running Environment)**:
某个靶场在当前机器上存在运行中的容器。因为一份 vulhub 检出对应一套固定的 compose 项目名，一个靶场最多只有一份运行中的实例，不存在"同一靶场跑多份"。
_Avoid_: 靶场实例、容器实例、实例

**compose 项目 (Compose Project)**:
`docker compose` 用来归拢一组容器、网络与卷的单位。靶场与 compose 项目一一对应：一个靶场无论内含几个容器，都只对应一个项目。
_Avoid_: 容器组、stack、应用

**vulhub 检出 (Vulhub Checkout)**:
`~/vulhub`，由 `init` 创建、由 `update` 维护的只读副本。用户不应直接修改它。
_Avoid_: 仓库、工作区、工作目录

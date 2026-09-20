# vulhub-cli

在一台机器上管理靶场生命周期的命令行工具。术语围绕三组概念展开：**靶场的身份**（怎么指代一个靶场）、**靶场的运行状态**（哪些正在跑）、以及**靶场的来源**（它从哪来）。

## Language

### 靶场的身份

**靶场 (Environment)**:
一个可运行的漏洞环境。它总是对应一个 compose 项目，因此"有几个靶场"与"有几个 compose 项目"是同一个问题。靶场可以是上游仓库里的一个目录，也可以是由本工具从镜像合成的环境——见**来源**。
_Avoid_: 漏洞环境、靶机、环境

**靶场路径 (Environment Path)**:
靶场唯一的身份标识，形如 `activemq/CVE-2023-46604` 或 `vulfocus/drupal-cve_2018_7600`。它同时编码了**来源**。
_Avoid_: 靶场名、靶场 ID

**靶场编号 (Environment Number)**:
分配给靶场的永久整数别名，让用户不必输入完整路径。一旦分配就不随上游更新而改变；靶场消失后该编号作废，不再复用。**所有来源共用同一套编号。**
_Avoid_: 序号、行号、ID、index

**索引 (Index)**:
靶场路径与靶场编号之间的持久映射。它是工具唯一需要持久保存的状态。
_Avoid_: 缓存、数据库、注册表

### 靶场的来源

**来源 (Source)**:
靶场的出处。靶场路径以 `vulfocus/` 开头者为 `vulfocus` 来源，其余为 `vulhub` 来源。

**vulhub 来源**:
来自上游 vulhub 仓库的靶场。它就是检出里的一个目录，`docker-compose.yml` 由上游提供。

**vulfocus 来源**:
来自 Docker Hub `vulfocus` 命名空间的靶场。上游只提供镜像，没有 compose 文件，因此环境定义由本工具**合成**。

**镜像清单 (Image Catalog)**:
vulfocus 来源的全部靶场及其端口。它由 `gen-vulfocus` 从 Docker Hub 生成，随即**内嵌进二进制分发**——用户不需要、也不应该自己去拉取它。
_Avoid_: 缓存、下载列表

**合成 compose 文件 (Synthesized Compose File)**:
为某个 vulfocus 靶场按需生成的 `docker-compose.yml`。它是纯派生数据，随时可从镜像清单重新生成，丢失无碍。
_Avoid_: 生成的配置、临时文件

### 靶场的运行状态

**运行中靶场 (Running Environment)**:
某个靶场在当前机器上存在运行中的容器。因为一份检出或一份合成目录对应一套固定的 compose 项目名，一个靶场最多只有一份运行中的实例，不存在"同一靶场跑多份"。
_Avoid_: 靶场实例、容器实例、实例

**compose 项目 (Compose Project)**:
`docker compose` 用来归拢一组容器、网络与卷的单位。靶场与 compose 项目一一对应：一个靶场无论内含几个容器，都只对应一个项目。
_Avoid_: 容器组、stack、应用

**vulhub 检出 (Vulhub Checkout)**:
`~/vulhub`，由 `init` 创建、由 `update` 维护的只读副本。用户不应直接修改它。
_Avoid_: 仓库、工作区、工作目录

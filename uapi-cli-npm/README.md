# Uapi CLI

**[Uapipro (uapis.cn)](https://uapis.cn)** 的 LLM 优先命令行工具与原生 SDK。

底层由 Go 原生编译，内置完整的 OpenAPI 索引。它不仅是开发者的终端 API 调试工具，更是**专为大语言模型和 AI Agent 设计的标准化工具执行工具哟**。

通过接入本 CLI，您可以零代码为您的 AI 赋予 UAPI 平台的全量 API 能力：允许 AI 自主检索可用接口、读取参数规范，并直接执行获取实时互联网数据、OCR 图像识别、文件上传与二进制媒体流下载等复杂任务。

## 特性

- **为 LLM 优化的输出结构**：非交互式（非 TTY）环境下，默认输出紧凑的 `llm` 格式 JSON，精准匹配大模型上下文窗口，大幅减少 Token 消耗。
- **极速冷启动**：Go 原生二进制执行，告别运行时解析 `openapi.yaml` 的性能损耗，满足 AI 高频并发调用的低延迟要求。
- **智能参数路由**：使用 `--set` 传参，AI 或开发者无需分辨参数位于 Query 还是 Body 中，底层自动完成规范化路由，极大降低 AI 构造请求的错误率。
- **开箱即用的多模态支持**：原生支持 Multipart 本地文件上传与二进制数据流存储（如图片下载），复杂请求结构支持读取 `.json` 文件。
- **多平台跨语言**：通过 npm 极简分发，并提供 Python 兼容包装层，无缝对接 LangChain / AutoGPT 等 AI 框架。

## 安装

**推荐：全局安装（需 Node.js 环境）**

```bash
npm i -g uapi-cli
```

**单次免安装执行**

```bash
npx uapi-cli discover ocr
```

**Python AI 框架兼容调用**

若在 Python 环境开发 AI 代理，可直接通过仓库提供的包装层调用：

```bash
python uapi_sdk_cli.py llm discover ocr
python uapi_llm_cli.py discover ocr
```

## 命令参考

CLI 提供的 4 个核心元命令，完整覆盖了 AI 自主探索与执行的闭环：

```bash
# 1. 服务发现：允许 AI 通过关键词搜索需要的接口
uapi discover bilibili

# 2. 获取目录：列出平台所有的 API 标签类目
uapi tags

# 3. 阅读规范：允许 AI 获取指定接口的参数结构与说明
uapi schema post-image-ocr

# 4. 执行调用：发起实际的 API 请求
uapi call get-network-myip
```

## 使用示例

### 基础调用与智能路由

利用 `--set` 模糊传参，CLI 自动将参数派发至正确位置：

```bash
uapi call get-social-bilibili-userinfo --set uid=1945126
```

### 缓存穿透

追加 `--disable-cache` 自动生成时间戳 `_t` 参数，强制请求最新数据：

```bash
uapi call get-social-bilibili-userinfo --set uid=1945126 --disable-cache
```

### OCR 图像识别（多模态）

**处理网络 URL 图片：**

```bash
uapi call post-image-ocr \
  --body url=https://uapis.cn/ocr-samples/bilingual-poetry-sample.png \
  --body need_location=false
```

**处理并上传本地图片文件：**

（注：使用 `--file` 声明文件流）

```bash
uapi call post-image-ocr \
  --file file=./demo.png \
  --body image_name=demo.png \
  --body need_location=false
```

### 二进制流调度与混合端点

以必应每日一图 API 为例，展示如何调度不同类型的返回格式。

**请求 JSON 结构化数据：**

```bash
uapi call get-image-bing-daily --query format=json --query resolution=1080
```

**直接下载二进制图片并保存：**

```bash
uapi call get-image-bing-daily --query resolution=1080 --out ./bing.jpg
```

### 复杂数据结构输入

当参数层级较深时，可直接挂载 JSON 文件：

```bash
uapi call post-image-ocr --json-file request.json
```

## 进阶配置

### 输出格式（`--format`）

- `llm`：非 TTY 环境默认。剔除冗余字段的精简 JSON 响应包，包含 `ok`、`operation` 和 `response.data`，专为 LLM 解析设计。
- `pretty`：TTY 终端默认。带有颜色高亮和缩进，适合人类开发者阅读。
- `json`：完整的调试级 JSON 输出。
- `raw`：仅输出原始 HTTP 响应体（若为二进制文件下载，则打印本地保存路径）。

### 请求预检（`--dry-run`）

仅用于验证 AI 生成的请求结构是否合法，返回标准化请求参数但不发起真实网络调用：

```bash
uapi call post-image-ocr --body need_location=false --dry-run
```

## 环境变量

| 变量 | 描述 |
| :--- | :--- |
| `UAPI_TOKEN` | UAPI Pro 平台认证 Token |
| `UAPI_BASE_URL` | 网关地址覆写（默认请求官方网关） |
| `UAPI_TIMEOUT` | 全局网络请求超时时间 |
| `UAPI_CLI_BINARY` | 自定义底层 Go 二进制可执行文件路径 |

## 源码与开发

如需参与底层 CLI 开发或自行编译：

```bash
cd uapi-cli-npm

# 编译适配当前平台的原生二进制文件
npm run build:local

# 本地调试调用
node ./bin/uapi.js discover ocr

# 为全平台交叉编译发布包
npm run build:release
```

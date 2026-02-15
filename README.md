# nagobot

nanobot 的 Go 重写版本。轻量级个人 AI 助手框架，支持 ReAct 工具调用循环和 Discord 频道接入。

## 构建

需要 Go 1.24+。

```bash
cd nagobot
go build -o nagobot ./cmd/nagobot/
```

编译产物为当前目录下的 `nagobot` 可执行文件。

也可以直接安装到 `$GOPATH/bin`：

```bash
go install ./cmd/nagobot/
```

## 初始化

首次使用前执行 `onboard`，创建配置文件和工作空间：

```bash
./nagobot onboard
```

生成的文件：

```
~/.nagobot/
├── config.json              # 配置文件
└── workspace/
    ├── AGENTS.md            # Agent 行为指令
    ├── SOUL.md              # 人格设定
    ├── USER.md              # 用户信息
    └── memory/
        └── MEMORY.md        # 长期记忆
```

## 配置

编辑 `~/.nagobot/config.json`，填入 LLM 提供商的 API Key。

### 使用 OpenRouter

```json
{
  "agents": {
    "defaults": {
      "model": "anthropic/claude-sonnet-4-5"
    }
  },
  "providers": {
    "openRouter": {
      "apiKey": "sk-or-v1-..."
    }
  }
}
```

### 使用 OpenAI

```json
{
  "agents": {
    "defaults": {
      "model": "gpt-4o"
    }
  },
  "providers": {
    "openAi": {
      "apiKey": "sk-...",
      "apiBase": "https://api.openai.com/v1"
    }
  }
}
```

### 使用 DeepSeek

```json
{
  "agents": {
    "defaults": {
      "model": "deepseek-chat"
    }
  },
  "providers": {
    "deepSeek": {
      "apiKey": "sk-...",
      "apiBase": "https://api.deepseek.com/v1"
    }
  }
}
```

支持任何 OpenAI 兼容的 API 端点，只需设置对应的 `apiKey` 和 `apiBase`。

### Discord 频道

```json
{
  "channels": {
    "discord": {
      "enabled": true,
      "token": "你的 Bot Token",
      "allowFrom": ["用户ID1", "用户ID2"]
    }
  }
}
```

`allowFrom` 为空数组时允许所有用户。

### 语音消息转文字

Discord 语音消息会自动通过 Google Cloud Speech-to-Text 转录为文字。需要：

1. 在 Google Cloud Console 启用 **Cloud Speech-to-Text API**
2. 创建 API Key

```json
{
  "services": {
    "googleStt": {
      "apiKey": "你的 Google Cloud API Key",
      "languageCode": "zh-CN"
    }
  }
}
```

`languageCode` 支持 [BCP-47 语言代码](https://cloud.google.com/speech-to-text/docs/languages)，默认为 `zh-CN`。未配置时语音消息不会被处理。

Discord 频道基于 [discordgo](https://github.com/bwmarrin/discordgo) SDK 实现，支持：

- 文本消息收发
- 语音消息转文字（通过 Google Cloud Speech-to-Text，需配置 `services.googleStt`）
- 文件/图片附件发送（通过 `message` 工具的 `files` 参数）
- 接收用户发送的附件（URL 传入 Agent）
- 消息回复引用
- 输入状态指示（typing indicator）

## 使用

### 单条消息

```bash
./nagobot agent -m "用 Go 写一个 Hello World"
```

### 交互模式

```bash
./nagobot agent
```

进入交互式对话，输入 `exit`、`quit` 或按 `Ctrl+C` 退出。

### 启动 Gateway

连接 Discord 频道，持续运行：

```bash
./nagobot gateway
```

### 查看状态

```bash
./nagobot status
```

显示配置路径、工作空间、模型、各提供商 API Key 状态和频道状态。

## 内置工具

Agent 在对话中可以调用以下工具：

| 工具 | 说明 |
|------|------|
| `read_file` | 读取文件内容 |
| `write_file` | 写入文件（自动创建目录） |
| `edit_file` | 查找替换编辑文件 |
| `list_dir` | 列出目录内容 |
| `exec` | 执行 shell 命令（带安全防护） |
| `message` | 向频道发送消息，支持附件（`files` 参数传入文件路径列表） |
| `spawn` | 后台派生子 Agent 执行长时间任务 |
| `web_search` | 网页搜索（Brave Search API） |
| `web_fetch` | 抓取网页内容并提取正文 |

`exec` 工具会拦截 `rm -rf`、`dd`、`shutdown` 等危险命令。

工具执行结果通过 `ToolResult` 结构返回，包含文本内容（`Content`）和可选的媒体文件路径（`Media`）。Agent 循环会收集所有工具产生的媒体文件，在最终回复时一并作为附件发送到频道。

## 架构

### 消息流

1. **入站**：消息从 CLI（`ProcessDirect`）或 Discord 频道到达，发布到 `bus.MessageBus` 入站通道。
2. **Agent 循环**（`internal/agent/loop.go`）：从入站通道读取消息，构建上下文（系统提示 + 会话历史），进入 ReAct 循环 — 调用 LLM，执行工具调用，注入反思提示，直到 LLM 返回无工具调用的最终响应。循环过程中收集工具产生的媒体文件。
3. **出站**：最终响应（含媒体附件）发布到出站通道，由 `MessageBus.DispatchOutbound` 路由到已订阅的频道处理器。

### 核心抽象

- **`llm.Provider`**（`internal/llm/provider.go`）：统一的 `Chat()` 接口。两个实现：
  - `OpenAIProvider` — 适配 OpenAI 兼容 API（OpenRouter、DeepSeek 等）
  - `AnthropicProvider` — 原生 Anthropic Messages API
- **`tool.Tool`**（`internal/tool/tool.go`）：`Name()`、`Description()`、`Parameters()`（JSON Schema）、`Execute()` 返回 `ToolResult`。通过 `Registry` 管理注册和执行。
- **`tool.ToolResult`**：工具执行结果，包含 `Content string`（文本，回传 LLM）和 `Media []string`（文件路径，附件发送到频道）。
- **`channel.Channel`**（`internal/channel/channel.go`）：`Start()`、`Stop()`、`Send()`。Discord 频道基于 discordgo SDK 实现。
- **`bus.MessageBus`**（`internal/bus/queue.go`）：通过 Go channel 和发布/订阅模式解耦频道与 Agent。

### 上下文与记忆

`ContextBuilder`（`internal/agent/context.go`）从以下来源组装系统提示：

- 运行时信息（时间、操作系统、工作空间路径）
- 工作空间引导文件：`AGENTS.md`、`SOUL.md`、`USER.md`、`TOOLS.md`、`IDENTITY.md`
- `MemoryStore`（`internal/agent/memory.go`）：读取 `memory/MEMORY.md`（长期记忆）和当日日期文件（每日笔记）

上下文过长时自动压缩：在 ReAct 循环中通过 `compressMessages` 对旧消息进行摘要，在会话切换间通过 `consolidateMemory` 整理到 `MEMORY.md`。

### 会话

`session.Manager`（`internal/session/manager.go`）将对话历史以 JSONL 文件持久化到 `~/.nagobot/sessions/`，会话以 `channel:chatID` 为键。

### 子 Agent

`SubagentManager`（`internal/agent/subagent.go`）支持通过 `spawn` 工具派生后台子 Agent 执行长时间任务，完成后通过系统消息将结果发布回原始频道。

## 项目结构

```
nagobot/
├── cmd/nagobot/main.go           # CLI 入口
├── internal/
│   ├── agent/
│   │   ├── loop.go               # ReAct 循环引擎
│   │   ├── context.go            # 系统提示词构建
│   │   ├── memory.go             # 文件记忆系统
│   │   ├── skills.go             # 技能加载器
│   │   └── subagent.go           # 后台子 Agent 系统
│   ├── bus/
│   │   ├── events.go             # 消息类型（InboundMessage / OutboundMessage）
│   │   └── queue.go              # Go channel 消息总线
│   ├── channel/
│   │   ├── channel.go            # Channel 接口
│   │   └── discord.go            # Discord 实现（discordgo SDK）
│   ├── cli/
│   │   ├── chat.go               # 交互式 TUI（bubbletea）
│   │   ├── onboard.go            # 初始化向导
│   │   ├── status.go             # 状态显示
│   │   └── styles.go             # 共享样式（lipgloss）
│   ├── config/
│   │   ├── config.go             # 配置结构体
│   │   └── loader.go             # JSON 加载/保存
│   ├── llm/
│   │   ├── provider.go           # LLM Provider 接口
│   │   ├── openai.go             # OpenAI 兼容实现
│   │   └── anthropic.go          # Anthropic 原生实现
│   ├── session/
│   │   └── manager.go            # JSONL 会话持久化
│   ├── stt/
│   │   └── google.go             # Google Cloud Speech-to-Text 转录
│   └── tool/
│       ├── tool.go               # Tool 接口、ToolResult、Registry
│       ├── filesystem.go         # 文件操作工具
│       ├── shell.go              # Shell 执行工具
│       ├── message.go            # 消息发送工具（含附件支持）
│       ├── spawn.go              # 子 Agent 派生工具
│       └── web.go                # 网页搜索/抓取工具
├── go.mod
└── go.sum
```

## 依赖

| 依赖 | 用途 |
|------|------|
| `github.com/bwmarrin/discordgo` | Discord Bot SDK |
| `github.com/charmbracelet/bubbletea` | 终端 TUI 框架 |
| `github.com/charmbracelet/lipgloss` | 终端样式 |
| `github.com/charmbracelet/bubbles` | TUI 组件（输入框、滚动视图等） |

## 配置项参考

```json
{
  "agents": {
    "defaults": {
      "workspace": "~/.nagobot/workspace",
      "model": "anthropic/claude-sonnet-4-5",
      "maxTokens": 8192,
      "temperature": 0.7,
      "maxToolIterations": 20,
      "memoryWindow": 50,
      "contextLimit": 80000
    }
  },
  "providers": {
    "anthropic": { "apiKey": "", "apiBase": "" },
    "openAi":    { "apiKey": "", "apiBase": "" },
    "openRouter": { "apiKey": "", "apiBase": "" },
    "deepSeek":  { "apiKey": "", "apiBase": "" },
    "gemini":    { "apiKey": "", "apiBase": "" }
  },
  "channels": {
    "discord": {
      "enabled": false,
      "token": "",
      "allowFrom": [],
      "intents": 37377
    }
  },
  "tools": {
    "web": {
      "search": { "apiKey": "" }
    },
    "exec": { "timeout": 60 },
    "restrictToWorkspace": false
  },
  "services": {
    "googleStt": {
      "apiKey": "",
      "languageCode": "zh-CN"
    }
  }
}
```

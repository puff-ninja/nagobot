# nagobot

nanobot 的 Go 重写版本。轻量级个人 AI 助手框架，支持 ReAct 工具调用循环和 Discord 频道接入。

## 构建

需要 Go 1.22+。

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
| `message` | 向频道发送消息 |

`exec` 工具会拦截 `rm -rf`、`dd`、`shutdown` 等危险命令。

## 项目结构

```
nagobot/
├── cmd/nagobot/main.go           # CLI 入口
├── internal/
│   ├── agent/
│   │   ├── loop.go               # ReAct 循环引擎
│   │   ├── context.go            # 系统提示词构建
│   │   └── memory.go             # 文件记忆系统
│   ├── bus/
│   │   ├── events.go             # 消息类型定义
│   │   └── queue.go              # Go channel 消息总线
│   ├── channel/
│   │   ├── channel.go            # Channel 接口
│   │   └── discord.go            # Discord Gateway + REST
│   ├── config/
│   │   ├── config.go             # 配置结构体
│   │   └── loader.go             # JSON 加载/保存
│   ├── llm/
│   │   ├── provider.go           # LLM Provider 接口
│   │   └── openai.go             # OpenAI 兼容实现
│   ├── session/
│   │   └── manager.go            # JSONL 会话持久化
│   └── tool/
│       ├── tool.go               # Tool 接口与 Registry
│       ├── filesystem.go         # 文件操作工具
│       ├── shell.go              # Shell 执行工具
│       └── message.go            # 消息发送工具
├── go.mod
└── go.sum
```

## 配置项参考

```json
{
  "agents": {
    "defaults": {
      "workspace": "~/.nagobot/workspace",
      "model": "anthropic/claude-sonnet-4-5",
      "maxTokens": 8192,
      "temperature": 0.7,
      "maxToolIterations": 20
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
      "gatewayUrl": "wss://gateway.discord.gg/?v=10&encoding=json",
      "intents": 37377
    }
  },
  "tools": {
    "exec": { "timeout": 60 },
    "restrictToWorkspace": false
  }
}
```

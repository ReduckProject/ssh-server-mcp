# SSH Server MCP

一个用于动态管理SSH服务器连接并执行命令的MCP（Model Context Protocol）服务器。

## 功能特性

- **动态注册SSH服务器** - 支持密码和私钥两种认证方式
- **配置文件加载** - 通过 `--config` 参数预加载服务器配置
- **执行远程命令** - 在已注册的服务器上执行任意命令
- **连接管理** - 自动重连、连接测试
- **安全隐藏** - 列出服务器时自动隐藏密码

## 安装构建

```bash
go build -o ssh-server-mcp.exe .
```

## 启动方式

### 1. MCP服务器模式（默认）
```bash
./ssh-server-mcp.exe --config=servers.csv
```

### 2. 交互式Shell模式
```bash
./ssh-server-mcp.exe --shell --config=servers.csv
```

交互式Shell提供类似XShell的体验：

```
╔════════════════════════════════════════════╗
║       SSH Server MCP - Interactive Shell   ║
╚════════════════════════════════════════════╝

输入 'help' 查看可用命令, 'exit' 退出

ssh-mcp> list
┌──────┬─────────────────────┬──────┬──────────┬─────────┐
│ 名称 │ 地址                │ 端口 │ 用户     │ 状态    │
├──────┼─────────────────────┼──────┼──────────┼─────────┤
│ prod │ 43.167.188.85       │ 22   │ root     │ 离线    │
└──────┴─────────────────────┴──────┴──────────┴─────────┘

ssh-mcp> connect prod
已连接到 prod (root@43.167.188.85:22)

prod> uname -a
Linux VM-0-7-opencloudos 6.6.117-45.1.oc9.x86_64 ...

prod> shell
# 进入真正的SSH终端模式

prod> exit
再见!
```

**交互式命令：**

| 命令 | 说明 |
|------|------|
| `list`, `ls` | 列出所有服务器 |
| `connect <name>` | 连接服务器 |
| `disconnect` | 断开当前连接 |
| `exec <cmd>` | 执行命令 |
| `shell` | 进入终端模式 |
| `add <name> <host> <port> <user> <pass>` | 添加服务器 |
| `remove <name>` | 移除服务器 |
| `info [name]` | 显示服务器信息 |
| `help` | 显示帮助 |
| `exit` | 退出 |

连接服务器后可直接输入命令执行。

## 配置文件格式

支持CSV和JSON两种格式，根据文件扩展名自动识别。

### CSV格式（推荐，易于编辑）

```csv
# SSH服务器配置文件
# 格式: name,host,port,user,password,keyFile
# password和keyFile二选一，不需要的字段留空
# 密码含逗号时用双引号包裹

prod-server,192.168.1.100,22,root,your-password,
dev-server,dev.example.com,22,admin,"pass,word",
key-server,10.0.0.1,2222,ubuntu,,~/.ssh/id_rsa
```

| 列 | 说明 |
|----|------|
| name | 服务器名称（唯一标识） |
| host | 服务器地址 |
| port | SSH端口（默认22） |
| user | 用户名 |
| password | 密码（含逗号需用引号包裹） |
| keyFile | 私钥文件路径 |

### JSON格式

```json
{
  "servers": [
    {
      "name": "prod-server",
      "host": "192.168.1.100",
      "port": 22,
      "user": "root",
      "password": "your-password"
    },
    {
      "name": "dev-server",
      "host": "dev.example.com",
      "user": "admin",
      "keyFile": "~/.ssh/id_rsa"
    }
  ]
}
```

## 提供的工具

### 1. `register_ssh_server`
注册一个新的SSH服务器连接。

**参数:**
| 参数 | 类型 | 必需 | 描述 |
|------|------|------|------|
| name | string | 是 | 服务器名称，用于后续引用 |
| host | string | 是 | 服务器地址 |
| port | number | 否 | SSH端口，默认22 |
| user | string | 是 | 用户名 |
| password | string | 否* | 密码（与keyFile二选一） |
| keyFile | string | 否* | 私钥文件路径（与password二选一） |

*密码和私钥必须提供其中之一

### 2. `unregister_ssh_server`
注销一个已注册的SSH服务器。

**参数:**
| 参数 | 类型 | 必需 | 描述 |
|------|------|------|------|
| name | string | 是 | 要注销的服务器名称 |

### 3. `list_ssh_servers`
列出所有已注册的SSH服务器。

### 4. `ssh_execute`
在指定的SSH服务器上执行命令。

**参数:**
| 参数 | 类型 | 必需 | 描述 |
|------|------|------|------|
| server | string | 是 | 服务器名称 |
| command | string | 是 | 要执行的命令 |
| timeout | number | 否 | 超时时间（秒），默认30秒 |

### 5. `test_ssh_connection`
测试指定SSH服务器的连接状态。

**参数:**
| 参数 | 类型 | 必需 | 描述 |
|------|------|------|------|
| name | string | 是 | 服务器名称 |

## 安装配置

### 构建

```bash
go build -o ssh-server-mcp.exe .
```

### 在Claude Desktop中配置

编辑Claude Desktop配置文件（Windows: `%APPDATA%\Claude\claude_desktop_config.json`）：

```json
{
  "mcpServers": {
    "ssh-server": {
      "command": "E:\\Code\\GitHub\\ssh-server-mcp\\ssh-server-mcp.exe",
      "args": ["--config=E:\\Code\\GitHub\\ssh-server-mcp\\servers.csv"]
    }
  }
}
```

### 在Claude Code中配置

编辑 `~/.claude/settings.json` 或使用 `/update-config` 命令：

```json
{
  "mcpServers": {
    "ssh-server": {
      "command": "E:\\Code\\GitHub\\ssh-server-mcp\\ssh-server-mcp.exe",
      "args": ["--config=E:\\Code\\GitHub\\ssh-server-mcp\\servers.csv"]
    }
  }
}
```

## 使用示例

### 注册服务器（密码认证）
```json
{
  "name": "register_ssh_server",
  "arguments": {
    "name": "my-server",
    "host": "192.168.1.100",
    "port": 22,
    "user": "admin",
    "password": "your-password"
  }
}
```

### 注册服务器（私钥认证）
```json
{
  "name": "register_ssh_server",
  "arguments": {
    "name": "prod-server",
    "host": "production.example.com",
    "user": "deploy",
    "keyFile": "C:\\Users\\YourName\\.ssh\\id_rsa"
  }
}
```

### 执行命令
```json
{
  "name": "ssh_execute",
  "arguments": {
    "server": "my-server",
    "command": "ls -la /var/log",
    "timeout": 60
  }
}
```

### 列出服务器
```json
{
  "name": "list_ssh_servers",
  "arguments": {}
}
```

### 测试连接
```json
{
  "name": "test_ssh_connection",
  "arguments": {
    "name": "my-server"
  }
}
```

### 注销服务器
```json
{
  "name": "unregister_ssh_server",
  "arguments": {
    "name": "my-server"
  }
}
```

## 安全注意事项

1. **密码存储** - 当前版本密码存储在内存中，服务器重启后需要重新注册
2. **主机密钥** - 当前接受所有主机密钥，生产环境建议启用严格验证
3. **权限控制** - 请谨慎使用此MCP，因为它允许在远程服务器上执行任意命令

## 技术架构

```
┌─────────────────┐     ┌──────────────────┐     ┌─────────────────┐
│   MCP Client    │────▶│   MCP Server     │────▶│  SSH Servers    │
│  (Claude/etc)   │     │  (this project)  │     │  (remote)       │
└─────────────────┘     └──────────────────┘     └─────────────────┘
                               │
                               ▼
                        ┌──────────────────┐
                        │ Server Manager   │
                        │ - Register       │
                        │ - Unregister     │
                        │ - Execute        │
                        │ - List           │
                        └──────────────────┘
```

## 许可证

MIT License
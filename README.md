# Augment2Api

Augment2Api 是一个用于连接 Augment API 的中间层服务，提供 OpenAI 兼容的接口，支持 Claude 3.7 模型的调用。

## 功能特点

- 提供 OpenAI 兼容的 API 接口
- 支持 Claude 3.7 模型
- 支持流式/非流式输出 (Stream/Non-Stream)
- 支持多 Token 管理和自动轮换
- 提供简洁的管理界面
- 支持 Redis 存储 Token

## 环境变量配置

| 环境变量 | 说明 | 是否必填 | 示例 |
|---------|------|---------|------|
| REDIS_CONN_STRING | Redis 连接字符串 | 是 | `redis://default:password@localhost:6379` |
| AUTH_TOKEN | API 访问认证 Token | 否 | `your-auth-token` |

## 快速开始

### 使用 Docker

## API 使用

### 获取模型
```bash
curl -X GET http://localhost:27080/v1/models
```

### 聊天接口
```bash
curl -X POST http://localhost:27080/v1/chat/completions \
-H "Content-Type: application/json" \
-d '{
"model": "claude-3.7",
"messages": [
{"role": "user", "content": "你好，请介绍一下自己"}
]
}'
```

## 管理界面

访问 `http://localhost:27080/` 可以打开管理界面，交互式获取、管理Token。

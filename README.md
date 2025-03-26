# Augment2Api

Augment2Api 是一个用于连接 Augment API 的中间层服务，提供 OpenAI 兼容的接口，支持 Claude 3.7 模型的调用。

## 功能特点

- 提供 OpenAI 兼容的 API 接口
- 支持 Claude 3.7 模型
- 支持流式/非流式输出 (Stream/Non-Stream)
- 支持简洁的多Token管理界面管理
- 支持 Redis 存储 Token

## 环境变量配置

| 环境变量 | 说明 | 是否必填 | 示例 |
|---------|------|---------|------|
| REDIS_CONN_STRING | Redis 连接字符串 | 是 | `redis://default:password@localhost:6379` |
| AUTH_TOKEN | API 访问认证 Token | 否 | `your-auth-token` |

## 快速开始

### 1. 部署

#### 使用 Docker 运行
```bash
docker run -d \
  --name augment2api \
  -p 27080:27080 \
  -e REDIS_CONN_STRING="redis://default:yourpassword@your-redis-host:6379" \
  -e AUTH_TOKEN="your-auth-token" \
  --restart always \
  linqiu1199/augment2api:latest
```

#### 使用 Docker Compose 运行
拉取项目到本地
```bash
git clone https://github.com/linqiu1199/augment2api.git
```

进入项目目录
```bash
cd augment2api
```


创建 `.env` 文件（可选，用于设置环境变量）：
```
REDIS_PASSWORD=your-secure-password
AUTH_TOKEN=your-secure-auth-token
```

然后运行：
```bash
docker-compose up -d
```

这将同时启动 Redis 和 Augment2Api 服务，并自动处理它们之间的网络连接。

### 2. 获取Token

访问http://ip:27080/进入管理页面：

1. 点击获取授权链接
2. 复制授权链接到浏览器中打开
3. 使用邮箱进行登录
4. 复制`augment code`到授权响应输入框中，点击获取token
5. 开始对话测试


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

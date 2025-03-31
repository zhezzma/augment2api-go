package api

import (
	"augment2api/config"
	"bytes"
	"encoding/json"
	"fmt"
	"math/rand"
	"net/http"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/go-redis/redis/v8"
	"github.com/google/uuid"
)

// TokenInfo 存储token信息
type TokenInfo struct {
	Token     string `json:"token"`
	TenantURL string `json:"tenant_url"`
}

// TokenItem token项结构
type TokenItem struct {
	Token     string `json:"token"`
	TenantUrl string `json:"tenantUrl"`
}

// TokenRequestStatus 记录 token 请求状态
type TokenRequestStatus struct {
	InProgress    bool      `json:"in_progress"`
	LastRequestAt time.Time `json:"last_request_at"`
}

// GetRedisTokenHandler 从Redis获取token列表
func GetRedisTokenHandler(c *gin.Context) {
	// 获取所有token的key (使用通配符模式)
	keys, err := config.RedisKeys("token:*")
	if err != nil {
		c.JSON(http.StatusOK, gin.H{
			"status": "error",
			"error":  "获取token列表失败: " + err.Error(),
		})
		return
	}

	// 如果没有token
	if len(keys) == 0 {
		c.JSON(http.StatusOK, gin.H{
			"status": "success",
			"tokens": []TokenInfo{},
		})
		return
	}

	// 构建token列表
	var tokenList []TokenInfo
	for _, key := range keys {
		// 从key中提取token (格式: "token:{token}")
		token := key[6:] // 去掉前缀 "token:"

		// 获取对应的tenant_url
		tenantURL, err := config.RedisHGet(key, "tenant_url")
		if err != nil {
			continue // 跳过无效的token
		}

		// 获取token状态
		status, err := config.RedisHGet(key, "status")
		if err == nil && status == "disabled" {
			continue // 跳过被标记为不可用的token
		}

		tokenList = append(tokenList, TokenInfo{
			Token:     token,
			TenantURL: tenantURL,
		})
	}

	c.JSON(http.StatusOK, gin.H{
		"status": "success",
		"tokens": tokenList,
	})
}

// SaveTokenToRedis 保存token到Redis
func SaveTokenToRedis(token, tenantURL string) error {
	// 创建一个唯一的key，包含token和tenant_url
	tokenKey := "token:" + token

	// 将tenant_url存储在token对应的哈希表中
	err := config.RedisHSet(tokenKey, "tenant_url", tenantURL)
	if err != nil {
		return err
	}

	// 默认将新添加的token标记为活跃状态
	return config.RedisHSet(tokenKey, "status", "active")
}

// GetRandomToken 从Redis中随机获取一个可用的token
func GetRandomToken() (string, string) {
	// 获取所有token的key
	keys, err := config.RedisKeys("token:*")
	if err != nil || len(keys) == 0 {
		return "", ""
	}

	// 筛选可用的token
	var availableTokens []string
	for _, key := range keys {
		// 获取token状态
		status, err := config.RedisHGet(key, "status")
		if err == nil && status == "disabled" {
			continue // 跳过被标记为不可用的token
		}
		availableTokens = append(availableTokens, key)
	}

	// 如果没有可用的token
	if len(availableTokens) == 0 {
		return "", ""
	}

	// 随机选择一个token
	randomIndex := rand.Intn(len(availableTokens))
	randomKey := availableTokens[randomIndex]

	// 从key中提取token
	token := randomKey[6:] // 去掉前缀 "token:"

	// 获取对应的tenant_url
	tenantURL, err := config.RedisHGet(randomKey, "tenant_url")
	if err != nil {
		return "", ""
	}

	return token, tenantURL
}

// DeleteTokenHandler 删除指定的token
func DeleteTokenHandler(c *gin.Context) {
	token := c.Param("token")
	if token == "" {
		c.JSON(http.StatusBadRequest, gin.H{
			"status": "error",
			"error":  "未指定token",
		})
		return
	}

	tokenKey := "token:" + token

	// 检查token是否存在
	exists, err := config.RedisExists(tokenKey)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"status": "error",
			"error":  "检查token失败: " + err.Error(),
		})
		return
	}

	if !exists {
		c.JSON(http.StatusNotFound, gin.H{
			"status": "error",
			"error":  "token不存在",
		})
		return
	}

	// 删除token
	if err := config.RedisDel(tokenKey); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"status": "error",
			"error":  "删除token失败: " + err.Error(),
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"status": "success",
	})
}

// UseTokenHandler 设置指定的token为当前活跃token
func UseTokenHandler(c *gin.Context) {
	token := c.Param("token")
	if token == "" {
		c.JSON(http.StatusBadRequest, gin.H{
			"status": "error",
			"error":  "未指定token",
		})
		return
	}

	tokenKey := "token:" + token

	// 检查token是否存在
	exists, err := config.RedisExists(tokenKey)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"status": "error",
			"error":  "检查token失败: " + err.Error(),
		})
		return
	}

	if !exists {
		c.JSON(http.StatusNotFound, gin.H{
			"status": "error",
			"error":  "token不存在",
		})
		return
	}

	// 设置当前活跃token
	if err := config.RedisSet("current_token", token, 0); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"status": "error",
			"error":  "设置当前token失败: " + err.Error(),
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"status": "success",
	})
}

// AddTokenHandler 批量添加token到Redis
func AddTokenHandler(c *gin.Context) {
	var tokens []TokenItem
	if err := c.ShouldBindJSON(&tokens); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"status": "error",
			"error":  "无效的请求数据",
		})
		return
	}

	// 检查是否有token数据
	if len(tokens) == 0 {
		c.JSON(http.StatusBadRequest, gin.H{
			"status": "error",
			"error":  "token列表为空",
		})
		return
	}

	// 批量保存token
	successCount := 0
	failedTokens := make([]string, 0)

	for _, item := range tokens {
		// 验证token格式
		if item.Token == "" || item.TenantUrl == "" {
			failedTokens = append(failedTokens, item.Token)
			continue
		}

		// 保存到Redis
		err := SaveTokenToRedis(item.Token, item.TenantUrl)
		if err != nil {
			failedTokens = append(failedTokens, item.Token)
			continue
		}
		successCount++
	}

	// 返回处理结果
	result := gin.H{
		"status":        "success",
		"total":         len(tokens),
		"success_count": successCount,
	}

	if len(failedTokens) > 0 {
		result["failed_tokens"] = failedTokens
		result["failed_count"] = len(failedTokens)
	}

	c.JSON(http.StatusOK, result)
}

// CheckTokenTenantURL 检测token的租户地址
func CheckTokenTenantURL(token string) (string, error) {
	// 构建测试消息
	testMsg := map[string]interface{}{
		"message":              "hello",
		"mode":                 "CHAT",
		"prefix":               "You are AI assistant,help me to solve problems!",
		"suffix":               " ",
		"lang":                 "HTML",
		"user_guidelines":      "You are a helpful assistant, you can help me to solve problems.",
		"workspace_guidelines": "",
		"feature_detection_flags": map[string]interface{}{
			"support_raw_output": true,
		},
		"tool_definitions": []map[string]interface{}{},
		"blobs": map[string]interface{}{
			"checkpoint_id": nil,
			"added_blobs":   []string{},
			"deleted_blobs": []string{},
		},
	}

	jsonData, err := json.Marshal(testMsg)
	if err != nil {
		return "", fmt.Errorf("序列化测试消息失败: %v", err)
	}

	tokenKey := "token:" + token

	// 测试不同的租户地址
	for i := 20; i >= 1; i-- {
		tenantURL := fmt.Sprintf("https://d%d.api.augmentcode.com/", i)

		// 创建请求
		req, err := http.NewRequest("POST", tenantURL+"chat-stream", bytes.NewReader(jsonData))
		if err != nil {
			continue
		}

		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+token)
		userAgents := []string{
			"augment.intellij/0.160.0 (Mac OS X; aarch64; 15.2) GoLand/2024.3.5",
			"augment.intellij/0.160.0 (Mac OS X; aarch64; 15.2) WebStorm/2024.3.5",
			"augment.intellij/0.160.0 (Mac OS X; aarch64; 15.2) PyCharm/2024.3.5",
		}
		req.Header.Set("User-Agent", userAgents[rand.Intn(len(userAgents))])
		req.Header.Set("x-api-version", "2")
		req.Header.Set("x-request-id", uuid.New().String())
		req.Header.Set("x-request-session-id", uuid.New().String())

		// 发送请求
		client := &http.Client{
			Timeout: 5 * time.Second,
		}
		resp, err := client.Do(req)
		if err != nil {
			continue
		}
		defer resp.Body.Close()

		// 检查是否返回401状态码（未授权）
		if resp.StatusCode == http.StatusUnauthorized {
			// 将token标记为不可用
			err = config.RedisHSet(tokenKey, "status", "disabled")
			if err != nil {
				fmt.Printf("标记token为不可用失败: %v\n", err)
			}
			fmt.Printf("token: %s 已被标记为不可用，返回401未授权\n", token)
			return "", fmt.Errorf("token未授权，已被标记为不可用")
		}

		// 检查响应状态
		if resp.StatusCode == http.StatusOK {
			// 尝试读取一小部分响应以确认是否有效
			buf := make([]byte, 1024)
			n, err := resp.Body.Read(buf)
			if err == nil && n > 0 {
				// 更新Redis中的租户地址和状态
				err = config.RedisHSet(tokenKey, "tenant_url", tenantURL)
				if err != nil {
					return "", fmt.Errorf("更新租户地址失败: %v", err)
				}
				// 将token标记为可用
				err = config.RedisHSet(tokenKey, "status", "active")
				if err != nil {
					fmt.Printf("标记token为可用失败: %v\n", err)
				}
				fmt.Printf("token: %s ,更新租户地址成功: %s\n", token, tenantURL)
				return tenantURL, nil
			}
		}
	}

	return "", fmt.Errorf("未找到有效的租户地址")
}

// CheckAllTokensHandler 批量检测所有token的租户地址
func CheckAllTokensHandler(c *gin.Context) {
	// 获取所有token的key
	keys, err := config.RedisKeys("token:*")
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"status": "error",
			"error":  "获取token列表失败: " + err.Error(),
		})
		return
	}

	if len(keys) == 0 {
		c.JSON(http.StatusOK, gin.H{
			"status":   "success",
			"total":    0,
			"updated":  0,
			"disabled": 0,
		})
		return
	}

	var wg sync.WaitGroup
	// 使用互斥锁保护计数器
	var mu sync.Mutex
	var updatedCount int
	var disabledCount int

	for _, key := range keys {
		wg.Add(1)
		go func(key string) {
			defer wg.Done()

			// 从key中提取token
			token := key[6:] // 去掉前缀 "token:"

			// 获取当前的租户地址
			oldTenantURL, _ := config.RedisHGet(key, "tenant_url")

			// 检测租户地址
			newTenantURL, err := CheckTokenTenantURL(token)
			fmt.Printf("token: %s ,当前租户地址: %s ,检测租户地址: %s\n", token, oldTenantURL, newTenantURL)

			mu.Lock()
			if err != nil && err.Error() == "token未授权，已被标记为不可用" {
				disabledCount++
			} else if err == nil && newTenantURL != oldTenantURL {
				updatedCount++
			}
			mu.Unlock()
		}(key)
	}

	wg.Wait()

	c.JSON(http.StatusOK, gin.H{
		"status":   "success",
		"total":    len(keys),
		"updated":  updatedCount,
		"disabled": disabledCount,
	})
}

// SetTokenRequestStatus 设置token请求状态
func SetTokenRequestStatus(token string, status TokenRequestStatus) error {
	// 使用Redis存储token请求状态
	key := "token_status:" + token

	// 将状态转换为JSON
	statusJSON, err := json.Marshal(status)
	if err != nil {
		return err
	}

	// 存储到Redis，设置过期时间为1小时
	return config.RedisSet(key, string(statusJSON), time.Hour)
}

// GetTokenRequestStatus 获取token请求状态
func GetTokenRequestStatus(token string) (TokenRequestStatus, error) {
	key := "token_status:" + token

	// 从Redis获取状态
	statusJSON, err := config.RedisGet(key)
	if err != nil {
		// 如果key不存在，返回默认状态
		if err == redis.Nil {
			return TokenRequestStatus{
				InProgress:    false,
				LastRequestAt: time.Time{}, // 零值时间
			}, nil
		}
		return TokenRequestStatus{}, err
	}

	// 解析JSON
	var status TokenRequestStatus
	if err := json.Unmarshal([]byte(statusJSON), &status); err != nil {
		return TokenRequestStatus{}, err
	}

	return status, nil
}

// GetAvailableToken 获取一个可用的token（未在使用中且冷却时间已过）
func GetAvailableToken() (string, string) {
	// 获取所有token的key
	keys, err := config.RedisKeys("token:*")
	if err != nil || len(keys) == 0 {
		return "", ""
	}

	// 筛选可用的token
	var availableTokens []string
	var availableTenantURLs []string

	for _, key := range keys {
		// 获取token状态
		status, err := config.RedisHGet(key, "status")
		if err == nil && status == "disabled" {
			continue // 跳过被标记为不可用的token
		}

		// 从key中提取token
		token := key[6:] // 去掉前缀 "token:"

		// 获取token的请求状态
		requestStatus, err := GetTokenRequestStatus(token)
		if err != nil {
			continue
		}

		// 如果token正在使用中，跳过
		if requestStatus.InProgress {
			continue
		}

		// 如果距离上次请求不足3秒，跳过
		if time.Since(requestStatus.LastRequestAt) < 3*time.Second {
			continue
		}

		// 获取对应的tenant_url
		tenantURL, err := config.RedisHGet(key, "tenant_url")
		if err != nil {
			continue
		}

		availableTokens = append(availableTokens, token)
		availableTenantURLs = append(availableTenantURLs, tenantURL)
	}

	// 如果没有可用的token
	if len(availableTokens) == 0 {
		return "", ""
	}

	// 随机选择一个token
	randomIndex := rand.Intn(len(availableTokens))
	return availableTokens[randomIndex], availableTenantURLs[randomIndex]
}

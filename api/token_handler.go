package api

import (
	"augment2api/config"
	"augment2api/pkg/logger"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"math/rand"
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/go-redis/redis/v8"
	"github.com/google/uuid"
)

// TokenInfo 存储token信息
type TokenInfo struct {
	Token           string `json:"token"`
	TenantURL       string `json:"tenant_url"`
	UsageCount      int    `json:"usage_count"`       // 总对话次数
	ChatUsageCount  int    `json:"chat_usage_count"`  // CHAT模式对话次数
	AgentUsageCount int    `json:"agent_usage_count"` // AGENT模式对话次数
	Remark          string `json:"remark"`            // 备注字段
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

// GetRedisTokenHandler 从Redis获取token列表，支持分页
func GetRedisTokenHandler(c *gin.Context) {
	// 获取分页参数（可选）
	page := c.DefaultQuery("page", "1")
	pageSize := c.DefaultQuery("page_size", "0") // 0表示不分页，返回所有

	pageNum, _ := strconv.Atoi(page)
	pageSizeNum, _ := strconv.Atoi(pageSize)

	if pageNum < 1 {
		pageNum = 1
	}

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
			"status":      "success",
			"tokens":      []TokenInfo{},
			"total":       0,
			"page":        pageNum,
			"page_size":   pageSizeNum,
			"total_pages": 0,
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

		// 获取备注信息
		remark, _ := config.RedisHGet(key, "remark")

		// 在获取token信息时，同时获取对话次数和备注
		tokenList = append(tokenList, TokenInfo{
			Token:           token,
			TenantURL:       tenantURL,
			UsageCount:      getTokenUsageCount(token),
			ChatUsageCount:  getTokenChatUsageCount(token),
			AgentUsageCount: getTokenAgentUsageCount(token),
			Remark:          remark,
		})
	}

	// 计算总页数和分页数据
	totalItems := len(tokenList)
	totalPages := 1

	// 如果需要分页
	if pageSizeNum > 0 {
		totalPages = (totalItems + pageSizeNum - 1) / pageSizeNum

		// 确保页码有效
		if pageNum > totalPages && totalPages > 0 {
			pageNum = totalPages
		}

		// 计算分页的起始和结束索引
		startIndex := (pageNum - 1) * pageSizeNum
		endIndex := startIndex + pageSizeNum

		if startIndex < totalItems {
			if endIndex > totalItems {
				endIndex = totalItems
			}
			tokenList = tokenList[startIndex:endIndex]
		} else {
			tokenList = []TokenInfo{}
		}
	}

	c.JSON(http.StatusOK, gin.H{
		"status":      "success",
		"tokens":      tokenList,
		"total":       totalItems,
		"page":        pageNum,
		"page_size":   pageSizeNum,
		"total_pages": totalPages,
	})
}

// SaveTokenToRedis 保存token到Redis
func SaveTokenToRedis(token, tenantURL string) error {
	// 创建一个唯一的key，包含token和tenant_url
	tokenKey := "token:" + token

	// token已存在，则跳过
	exists, err := config.RedisExists(tokenKey)
	if err != nil {
		return err
	}
	if exists {
		return nil
	}

	// 将tenant_url存储在token对应的哈希表中
	err = config.RedisHSet(tokenKey, "tenant_url", tenantURL)
	if err != nil {
		return err
	}

	// 默认将新添加的token标记为活跃状态
	err = config.RedisHSet(tokenKey, "status", "active")
	if err != nil {
		return err
	}

	// 初始化备注为空字符串
	return config.RedisHSet(tokenKey, "remark", "")
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

	// 删除token关联的使用次数（如果存在）
	// 删除总使用次数
	tokenUsageKey := "token_usage:" + token
	exists, err = config.RedisExists(tokenUsageKey)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"status": "error",
			"error":  "检查token使用次数失败: " + err.Error(),
		})
		return
	}
	if exists {
		if err := config.RedisDel(tokenUsageKey); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{
				"status": "error",
				"error":  "删除token使用次数失败: " + err.Error(),
			})
		}
	}

	// 删除CHAT模式使用次数
	tokenChatUsageKey := "token_usage_chat:" + token
	exists, err = config.RedisExists(tokenChatUsageKey)
	if err == nil && exists {
		config.RedisDel(tokenChatUsageKey)
	}

	// 删除AGENT模式使用次数
	tokenAgentUsageKey := "token_usage_agent:" + token
	exists, err = config.RedisExists(tokenAgentUsageKey)
	if err == nil && exists {
		config.RedisDel(tokenAgentUsageKey)
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
		"message":              "hello，what is your name",
		"mode":                 "CHAT",
		"prefix":               "You are AI assistant,help me to solve problems!",
		"suffix":               " ",
		"lang":                 "HTML",
		"user_guidelines":      "You are a helpful assistant, you can help me to solve problems and always answer in Chinese.",
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

	currentTenantURL, err := config.RedisHGet(tokenKey, "tenant_url")

	var tenantURLResult string
	var foundValid bool
	var tenantURLsToTest []string

	// 如果Redis中有有效的租户地址，优先测试该地址
	if err == nil && currentTenantURL != "" {
		tenantURLsToTest = append(tenantURLsToTest, currentTenantURL)
	}

	// 添加其他租户地址
	for i := 20; i >= 1; i-- {
		newTenantURL := fmt.Sprintf("https://d%d.api.augmentcode.com/", i)
		// 避免重复测试已有的租户地址
		if newTenantURL != currentTenantURL {
			tenantURLsToTest = append(tenantURLsToTest, newTenantURL)
		}
	}

	// 测试租户地址
	for _, tenantURL := range tenantURLsToTest {
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

		client := createHTTPClient()
		resp, err := client.Do(req)
		if err != nil {
			fmt.Printf("请求失败: %v\n", err)
			continue
		}

		isInvalid := false
		func() {
			defer resp.Body.Close()

			// 检查是否返回401状态码（未授权）
			if resp.StatusCode == http.StatusUnauthorized {
				// 读取响应体内容
				buf := make([]byte, 1024)
				n, readErr := resp.Body.Read(buf)
				responseBody := ""
				if readErr == nil && n > 0 {
					responseBody = string(buf[:n])
				}

				// 只有当响应中包含"Invalid token"时才标记为不可用
				if readErr == nil && n > 0 && bytes.Contains(buf[:n], []byte("Invalid token")) {
					// 将token标记为不可用
					err = config.RedisHSet(tokenKey, "status", "disabled")
					if err != nil {
						fmt.Printf("标记token为不可用失败: %v\n", err)
					}
					logger.Log.Info("token: %s 已被标记为不可用,返回401未授权,错误信息: %s\n", token, responseBody)
					isInvalid = true
				}
				return
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
						return
					}
					// 将token标记为可用
					err = config.RedisHSet(tokenKey, "status", "active")
					if err != nil {
						fmt.Printf("标记token为可用失败: %v\n", err)
					}
					logger.Log.Info("token: %s ,更新租户地址成功: %s\n", token, tenantURL)
					tenantURLResult = tenantURL
					foundValid = true
				}
			}
		}()

		// 如果token无效，立即返回错误，不再测试其他地址
		if isInvalid {
			return "", fmt.Errorf("token被标记为不可用")
		}

		// 如果找到有效的租户地址，跳出循环
		if foundValid {
			return tenantURLResult, nil
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
		// 获取token状态，跳过已标记为不可用的token
		status, err := config.RedisHGet(key, "status")
		if err == nil && status == "disabled" {
			mu.Lock()
			mu.Unlock()
			continue // 跳过此token
		}

		wg.Add(1)
		go func(key string) {
			defer wg.Done()

			// 从key中提取token
			token := key[6:] // 去掉前缀 "token:"

			// 获取当前的租户地址
			oldTenantURL, _ := config.RedisHGet(key, "tenant_url")

			// 检测租户地址
			newTenantURL, err := CheckTokenTenantURL(token)
			logger.Log.Info("token: %s ,当前租户地址: %s ,检测租户地址: %s\n", token, oldTenantURL, newTenantURL)

			mu.Lock()
			if err != nil && err.Error() == "token被标记为不可用" {
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
		if errors.Is(err, redis.Nil) {
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
		return "No token", ""
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

		// 检查CHAT模式和AGENT模式的使用次数限制
		chatUsageCount := getTokenChatUsageCount(token)
		agentUsageCount := getTokenAgentUsageCount(token)

		// 如果CHAT模式已达到3000次限制，跳过
		if chatUsageCount >= 3000 {
			continue
		}

		// 如果AGENT模式已达到50次限制，跳过
		if agentUsageCount >= 50 {
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
		return "No available token", ""
	}

	// 随机选择一个token
	randomIndex := rand.Intn(len(availableTokens))
	return availableTokens[randomIndex], availableTenantURLs[randomIndex]
}

// getTokenUsageCount 获取token的使用次数
func getTokenUsageCount(token string) int {
	// 使用Redis中的计数器获取使用次数
	countKey := "token_usage:" + token
	count, err := config.RedisGet(countKey)
	if err != nil {
		return 0 // 如果出错或不存在，返回0
	}

	// 将字符串转换为整数
	countInt, err := strconv.Atoi(count)
	if err != nil {
		return 0
	}

	return countInt
}

// getTokenChatUsageCount 获取token的CHAT模式使用次数
func getTokenChatUsageCount(token string) int {
	// 使用Redis中的计数器获取使用次数
	countKey := "token_usage_chat:" + token
	count, err := config.RedisGet(countKey)
	if err != nil {
		return 0 // 如果出错或不存在，返回0
	}

	// 将字符串转换为整数
	countInt, err := strconv.Atoi(count)
	if err != nil {
		return 0
	}

	return countInt
}

// getTokenAgentUsageCount 获取token的AGENT模式使用次数
func getTokenAgentUsageCount(token string) int {
	// 使用Redis中的计数器获取使用次数
	countKey := "token_usage_agent:" + token
	count, err := config.RedisGet(countKey)
	if err != nil {
		return 0 // 如果出错或不存在，返回0
	}

	// 将字符串转换为整数
	countInt, err := strconv.Atoi(count)
	if err != nil {
		return 0
	}

	return countInt
}

// UpdateTokenRemark 更新token的备注信息
func UpdateTokenRemark(c *gin.Context) {
	token := c.Param("token")
	if token == "" {
		c.JSON(http.StatusBadRequest, gin.H{
			"status": "error",
			"error":  "未指定token",
		})
		return
	}

	var req struct {
		Remark string `json:"remark"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"status": "error",
			"error":  "无效的请求数据",
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

	// 更新备注
	err = config.RedisHSet(tokenKey, "remark", req.Remark)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"status": "error",
			"error":  "更新备注失败: " + err.Error(),
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"status": "success",
	})
}

// MigrateTokensRemark 确保所有token都有remark字段
func MigrateTokensRemark() error {
	// 获取所有token的key
	keys, err := config.RedisKeys("token:*")
	if err != nil {
		return fmt.Errorf("获取token列表失败: %v", err)
	}

	for _, key := range keys {
		// 检查是否已有remark字段
		exists, err := config.RedisHExists(key, "remark")
		if err != nil {
			logger.Log.Error("check remark field of token %s failed: %v", key, err)
			continue
		}

		// 如果没有remark字段，添加一个空的remark
		if !exists {
			err = config.RedisHSet(key, "remark", "")
			if err != nil {
				logger.Log.Error("add remark field to token %s failed: %v", key, err)
				continue
			}
			logger.Log.Info("add remark field to token %s success", key)
		}
	}
	logger.Log.Info("migrate remark field to all tokens success!")

	return nil
}

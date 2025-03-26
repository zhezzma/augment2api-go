package api

import (
	"augment2api/config"
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
)

// OpenAIRequest OpenAI兼容的请求结构
type OpenAIRequest struct {
	Model       string        `json:"model,omitempty"`
	Messages    []ChatMessage `json:"messages,omitempty"`
	Stream      bool          `json:"stream,omitempty"`
	Temperature float64       `json:"temperature,omitempty"`
	MaxTokens   int           `json:"max_tokens,omitempty"`
}

// OpenAIResponse OpenAI兼容的响应结构
type OpenAIResponse struct {
	ID      string   `json:"id"`
	Object  string   `json:"object"`
	Created int64    `json:"created"`
	Model   string   `json:"model"`
	Choices []Choice `json:"choices"`
	Usage   Usage    `json:"usage"`
}

// OpenAIStreamResponse OpenAI兼容的流式响应结构
type OpenAIStreamResponse struct {
	ID      string         `json:"id"`
	Object  string         `json:"object"`
	Created int64          `json:"created"`
	Model   string         `json:"model"`
	Choices []StreamChoice `json:"choices"`
}

type StreamChoice struct {
	Index        int         `json:"index"`
	Delta        ChatMessage `json:"delta"`
	FinishReason *string     `json:"finish_reason"`
}

type Choice struct {
	Index        int         `json:"index"`
	Message      ChatMessage `json:"message"`
	FinishReason *string     `json:"finish_reason"`
}

type ChatMessage struct {
	Role    string      `json:"role"`
	Content interface{} `json:"content"`
}

// GetContent 添加一个辅助方法来获取消息内容
func (m ChatMessage) GetContent() string {
	switch v := m.Content.(type) {
	case string:
		return v
	case []interface{}:
		var result string
		for _, item := range v {
			if contentMap, ok := item.(map[string]interface{}); ok {
				if text, exists := contentMap["text"]; exists {
					if textStr, ok := text.(string); ok {
						result += textStr
					}
				}
			}
		}
		return result
	default:
		return ""
	}
}

type Usage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`
}

// AugmentRequest Augment API请求结构
type AugmentRequest struct {
	ChatHistory []AugmentChatHistory `json:"chat_history"`
	Message     string               `json:"message"`
	Mode        string               `json:"mode"`
}

type AugmentChatHistory struct {
	ResponseText   string `json:"response_text"`
	RequestMessage string `json:"request_message"`
}

// AugmentResponse Augment API响应结构
type AugmentResponse struct {
	Text string `json:"text"`
	Done bool   `json:"done"`
}

// CodeResponse 用于解析从授权服务返回的代码
type CodeResponse struct {
	Code      string `json:"code"`
	State     string `json:"state"`
	TenantURL string `json:"tenant_url"`
}

// ModelObject OpenAI模型对象结构
type ModelObject struct {
	ID      string `json:"id"`
	Object  string `json:"object"`
	Created int    `json:"created"`
	OwnedBy string `json:"owned_by"`
}

// ModelsResponse OpenAI模型列表响应结构
type ModelsResponse struct {
	Object string        `json:"object"`
	Data   []ModelObject `json:"data"`
}

// 全局变量
var (
	accessToken string
	tenantURL   string
)

// SetAuthInfo 设置认证信息
func SetAuthInfo(token, tenant string) {
	accessToken = token
	tenantURL = tenant
}

// GetAuthInfo 获取认证信息
func GetAuthInfo() (string, string) {
	if config.AppConfig.CodingMode == "true" {
		// 调试模式
		return config.AppConfig.CodingToken, config.AppConfig.TenantURL
	}

	// 随机获取一个token
	token, tenantURL := GetRandomToken()
	if token != "" && tenantURL != "" {
		return token, tenantURL
	}

	// 如果没有可用的token，则使用内存中的token
	return accessToken, tenantURL
}

// convertToAugmentRequest 将OpenAI请求转换为Augment请求
func convertToAugmentRequest(req OpenAIRequest) AugmentRequest {
	augmentReq := AugmentRequest{
		Mode: "CHAT",
	}

	if len(req.Messages) > 0 {
		lastMsg := req.Messages[len(req.Messages)-1]
		augmentReq.Message = lastMsg.GetContent()
	}

	var history []AugmentChatHistory
	for i := 0; i < len(req.Messages)-1; i += 2 {
		if i+1 < len(req.Messages) {
			history = append(history, AugmentChatHistory{
				RequestMessage: req.Messages[i].GetContent(),
				ResponseText:   req.Messages[i+1].GetContent(),
			})
		}
	}

	augmentReq.ChatHistory = history
	return augmentReq
}

// AuthHandler 处理授权请求
func AuthHandler(c *gin.Context, authorizeURL string) {
	c.JSON(http.StatusOK, gin.H{
		"authorize_url": authorizeURL,
	})
}

// CallbackHandler 处理回调请求
func CallbackHandler(c *gin.Context, getAccessTokenFunc func(string, string, string) (string, error)) {
	// 1. 解析请求数据
	var codeResp CodeResponse
	if err := c.ShouldBindJSON(&codeResp); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "无效的请求数据"})
		return
	}

	// 2. 使用授权码获取访问令牌
	token, err := getAccessTokenFunc(codeResp.TenantURL, "", codeResp.Code)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	// 3. 保存令牌和租户URL
	SetAuthInfo(token, codeResp.TenantURL)

	// 4. 保存到Redis
	if err := SaveTokenToRedis(token, codeResp.TenantURL); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "保存token到Redis失败: " + err.Error()})
		return
	}

	// 5. 返回成功响应
	c.JSON(http.StatusOK, gin.H{
		"status": "success",
		"token":  token,
	})
}

// ModelsHandler 处理模型请求
func ModelsHandler(c *gin.Context) {
	// 创建符合OpenAI格式的模型列表响应
	response := ModelsResponse{
		Object: "list",
		Data: []ModelObject{
			{
				ID:      "claude-3-7-sonnet-20250219",
				Object:  "model",
				Created: 1708387201,
				OwnedBy: "anthropic",
			},
			{
				ID:      "claude-3.7",
				Object:  "model",
				Created: 1708387200,
				OwnedBy: "anthropic",
			},
		},
	}

	c.JSON(http.StatusOK, response)
}

// ChatCompletionsHandler 处理聊天完成请求
func ChatCompletionsHandler(c *gin.Context) {
	token, tenant := GetAuthInfo()
	if token == "" || tenant == "" {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "无可用Token,请先在管理页面获取"})
		return
	}

	var openAIReq OpenAIRequest
	if err := c.ShouldBindJSON(&openAIReq); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "无效的请求数据"})
		return
	}

	// 转换为Augment请求格式
	augmentReq := convertToAugmentRequest(openAIReq)

	// 处理流式请求
	if openAIReq.Stream {
		handleStreamRequest(c, augmentReq, openAIReq.Model)
		return
	}

	// 处理非流式请求
	handleNonStreamRequest(c, augmentReq, openAIReq.Model)
}

// 处理流式请求
func handleStreamRequest(c *gin.Context, augmentReq AugmentRequest, model string) {
	c.Header("Content-Type", "text/event-stream")
	c.Header("Cache-Control", "no-cache")
	c.Header("Connection", "keep-alive")

	// 获取token和tenant_url
	token, tenant := GetAuthInfo()
	if token == "" || tenant == "" {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "无可用Token,请先在管理页面获取"})
		return
	}

	// 准备请求数据
	jsonData, err := json.Marshal(augmentReq)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "序列化请求失败"})
		return
	}

	// 创建请求 - 使用获取到的tenant_url
	req, err := http.NewRequest("POST", tenant+"chat-stream", strings.NewReader(string(jsonData)))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "创建请求失败"})
		return
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token) // 使用获取到的token

	// 发送请求
	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "请求失败: " + err.Error()})
		return
	}
	defer resp.Body.Close()

	// 设置刷新器以确保数据立即发送
	flusher, ok := c.Writer.(http.Flusher)
	if !ok {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "流式传输不支持"})
		return
	}

	// 读取并转发响应
	reader := bufio.NewReader(resp.Body)
	responseID := fmt.Sprintf("chatcmpl-%d", time.Now().Unix())

	var fullText string
	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			if err == io.EOF {
				break
			}
			log.Printf("读取响应失败: %v", err)
			break
		}

		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}

		var augmentResp AugmentResponse
		if err := json.Unmarshal([]byte(line), &augmentResp); err != nil {
			log.Printf("解析响应失败: %v", err)
			continue
		}

		fullText += augmentResp.Text

		// 创建OpenAI兼容的流式响应
		streamResp := OpenAIStreamResponse{
			ID:      responseID,
			Object:  "chat.completion.chunk",
			Created: time.Now().Unix(),
			Model:   model,
			Choices: []StreamChoice{
				{
					Index: 0,
					Delta: ChatMessage{
						Role:    "assistant",
						Content: augmentResp.Text,
					},
					FinishReason: nil,
				},
			},
		}

		// 如果是最后一条消息，设置完成原因
		if augmentResp.Done {
			finishReason := "stop"
			streamResp.Choices[0].FinishReason = &finishReason
		}

		// 序列化并发送响应
		jsonResp, err := json.Marshal(streamResp)
		if err != nil {
			log.Printf("序列化响应失败: %v", err)
			continue
		}

		fmt.Fprintf(c.Writer, "data: %s\n\n", jsonResp)
		flusher.Flush()

		// 如果完成，发送最后的[DONE]标记
		if augmentResp.Done {
			fmt.Fprintf(c.Writer, "data: [DONE]\n\n")
			flusher.Flush()
			break
		}
	}
}

// estimateTokenCount 粗略估计文本中的token数量
// 这是一个简单的估算方法，实际token数量取决于具体的分词算法
func estimateTokenCount(text string) int {
	// 英文单词和标点符号大约是1个token
	// 中文字符大约是1.5个token（每个字符约为0.75个token）
	// 按空格分割英文单词
	words := strings.Fields(text)
	wordCount := len(words)

	// 计算中文字符数量
	chineseCount := 0
	for _, r := range text {
		if r >= 0x4E00 && r <= 0x9FFF {
			chineseCount++
		}
	}

	// 粗略估计：英文单词按1个token计算，中文字符按0.75个token计算
	return wordCount + int(float64(chineseCount)*0.75)
}

// 处理非流式请求
func handleNonStreamRequest(c *gin.Context, augmentReq AugmentRequest, model string) {
	// 获取token和tenant_url
	token, tenant := GetAuthInfo()
	if token == "" || tenant == "" {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "无可用Token,请先在管理页面获取"})
		return
	}

	// 准备请求数据
	jsonData, err := json.Marshal(augmentReq)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "序列化请求失败"})
		return
	}

	// 创建请求 - 使用获取到的tenant_url
	req, err := http.NewRequest("POST", tenant+"chat-stream", strings.NewReader(string(jsonData)))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "创建请求失败"})
		return
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token) // 使用获取到的token

	// 发送请求
	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "请求失败: " + err.Error()})
		return
	}
	defer resp.Body.Close()

	// 读取完整响应
	reader := bufio.NewReader(resp.Body)
	var fullText string

	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			if err == io.EOF {
				break
			}
			c.JSON(http.StatusInternalServerError, gin.H{"error": "读取响应失败: " + err.Error()})
			return
		}

		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}

		var augmentResp AugmentResponse
		if err := json.Unmarshal([]byte(line), &augmentResp); err != nil {
			continue
		}

		fullText += augmentResp.Text

		if augmentResp.Done {
			break
		}
	}

	// 创建OpenAI兼容的响应
	finishReason := "stop"

	// 估算token数量
	promptTokens := estimateTokenCount(augmentReq.Message)
	for _, history := range augmentReq.ChatHistory {
		promptTokens += estimateTokenCount(history.RequestMessage)
		promptTokens += estimateTokenCount(history.ResponseText)
	}
	completionTokens := estimateTokenCount(fullText)

	openAIResp := OpenAIResponse{
		ID:      fmt.Sprintf("chatcmpl-%d", time.Now().Unix()),
		Object:  "chat.completion",
		Created: time.Now().Unix(),
		Model:   model,
		Choices: []Choice{
			{
				Index: 0,
				Message: ChatMessage{
					Role:    "assistant",
					Content: fullText,
				},
				FinishReason: &finishReason,
			},
		},
		Usage: Usage{
			PromptTokens:     promptTokens,
			CompletionTokens: completionTokens,
			TotalTokens:      promptTokens + completionTokens,
		},
	}

	c.JSON(http.StatusOK, openAIResp)
}

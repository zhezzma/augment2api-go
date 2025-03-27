package config

import (
	"augment2api/pkg/logger"
	"os"
)

type Config struct {
	RedisConnString string
	AuthToken       string
	CodingMode      string
	CodingToken     string
	TenantURL       string
	RoutePrefix     string
}

var AppConfig Config

func InitConfig() error {
	// 从环境变量读取配置
	AppConfig = Config{
		// 必填配置
		RedisConnString: getEnv("REDIS_CONN_STRING", ""),
		// 非必填配置
		AuthToken:   getEnv("AUTH_TOKEN", ""),
		CodingMode:  getEnv("CODING_MODE", "false"),
		CodingToken: getEnv("CODING_TOKEN", ""),
		TenantURL:   getEnv("TENANT_URL", ""),
		RoutePrefix: getEnv("ROUTE_PREFIX", ""), // 自定义openai接口路由前缀
	}

	// redis连接字符串 示例: redis://default:pwd@localhost:6379
	if AppConfig.RedisConnString == "" {
		logger.Log.Fatalln("未配置环境变量 REDIS_CONN_STRING")
	}

	logger.Log.Info("Augment2Api配置加载完成:\n" +
		"----------------------------------------\n" +
		"AuthToken:    " + AppConfig.AuthToken + "\n" +
		"RedisConnString: " + AppConfig.RedisConnString + "\n" +
		"----------------------------------------")

	return nil
}

func getEnv(key, defaultValue string) string {
	value := os.Getenv(key)
	if value == "" {
		return defaultValue
	}
	return value
}

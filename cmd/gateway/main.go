package main

import (
	"bufio"
	"fmt"
	"gateway/cmd/config"
	"gateway/cmd/utils"
	"io"
	"log"
	"net/http"
	"strings"

	dapr "github.com/dapr/go-sdk/client"
	"github.com/gin-gonic/gin"
	"golang.org/x/time/rate"
	"google.golang.org/grpc/metadata" // 必须引入这个包
)

var (
	daprClient dapr.Client
	// 针对 AppID 的限流器，每秒 50 个请求，桶大小 100
	limiter = rate.NewLimiter(50, 100)
)

func main() {
	// 1. 初始化 Dapr 客户端
	client, err := dapr.NewClient()
	if err != nil {
		log.Fatalf("无法连接到 Dapr Sidecar: %v", err)
	}
	daprClient = client
	defer daprClient.Close()
	// 初始化配置
	config.InitConfig()
	r := gin.Default()

	// 2. 跨域处理
	r.Use(CORSMiddleware())

	// 3. 通用网关逻辑：支持任意 /:appID/*method
	// 例如：POST /ticket-service/api/buy -> 调用 AppID 为 ticket-service 的 api/buy 方法
	r.Any("/:appID/*method", RateLimitMiddleware(), AuthMiddleware(), func(c *gin.Context) {
		appID := c.Param("appID")
		method := c.Param("method")
		method = strings.TrimPrefix(method, "/")

		// 检查是否是 SSE (流式) 请求
		if c.GetHeader("Accept") == "text/event-stream" {
			handleStreamRequest(c, appID, method)
		} else {
			handleStandardRequest(c, appID, method)
		}
	})

	log.Println("Go 网关启动在 :8081 端口...")
	r.Run(":8081")
}

// --- 处理普通请求 (HTTP 转 gRPC) ---
func handleStandardRequest(c *gin.Context, appID, method string) {
	body, _ := io.ReadAll(c.Request.Body)

	// 构建元数据（gRPC Metadata），透传给 Java 端
	md := metadata.Pairs(
		"x-user-id", c.GetString("userId"),
		"x-user-role", c.GetString("role"),
	)
	ctx := metadata.NewOutgoingContext(c.Request.Context(), md)

	content := &dapr.DataContent{
		Data:        body,
		ContentType: c.ContentType(),
	}

	// 核心：动态调用 Dapr Service Invocation
	resp, err := daprClient.InvokeMethodWithContent(
		ctx,
		appID,
		method,
		c.Request.Method,
		content,
	)

	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": "后端调用失败", "details": err.Error()})
		return
	}

	c.Data(http.StatusOK, "application/json", resp)
}

// --- 处理 SSE 流式请求 (AI 逐字渲染适配) ---
func handleStreamRequest(c *gin.Context, appID, method string) {
	// SSE 需要保持长连接，直接通过 Dapr 的 HTTP 接口进行代理，以支持流
	// Dapr Sidecar 默认 HTTP 端口通常是 3500
	daprUrl := fmt.Sprintf("http://localhost:3500/v1.0/invoke/%s/method/%s", appID, method)

	req, _ := http.NewRequest(c.Request.Method, daprUrl, c.Request.Body)
	req.Header = c.Request.Header

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		c.Status(http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()

	// 设置 SSE 响应头
	c.Writer.Header().Set("Content-Type", "text/event-stream")
	c.Writer.Header().Set("Cache-Control", "no-cache")
	c.Writer.Header().Set("Connection", "keep-alive")

	// 边读边写：将 Java 返回的 Chunk 实时刷给前端
	reader := bufio.NewReader(resp.Body)
	c.Stream(func(w io.Writer) bool {
		line, err := reader.ReadBytes('\n')
		if err != nil {
			return false
		}
		w.Write(line)
		return true
	})
}

// --- 中间件部分 ---
func AuthMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		appID := c.Param("appID")
		method := c.Param("method")
		requiredAuth := config.GetRequiredAuth(appID, method, c.Request.Method)
		if requiredAuth == "Empty" {
			c.Next()
			return
		}
		token := c.GetHeader("Authorization")
		// 验证解析token
		claims, err := utils.ValidateJWT(token)
		if err != nil {
			log.Printf("JWT 解析失败: %v", err)
			c.JSON(401, gin.H{"msg": "身份验证失败"})
			c.Abort()
			return
		}
		// 如果需要 Admin 权限，额外检查角色
		if requiredAuth == "Admin" && claims.Role != "ADMIN" {
			c.JSON(403, gin.H{"msg": "权限不足，需要管理员角色"})
			c.Abort()
			return
		}
		log.Printf("用户 %s 角色 %s 访问 %s/%s 成功", claims.UserId, claims.Role, appID, method)
		c.Set("userId", claims.UserId)
		c.Set("username", claims.Username)
		c.Set("role", claims.Role)
		c.Next()
	}
}

func RateLimitMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		if !limiter.Allow() {
			c.JSON(http.StatusTooManyRequests, gin.H{"error": "请求过快，请稍后再试"})
			c.Abort()
			return
		}
		c.Next()
	}
}

func CORSMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Writer.Header().Set("Access-Control-Allow-Origin", "*")
		c.Writer.Header().Set("Access-Control-Allow-Methods", "POST, GET, OPTIONS, PUT, DELETE")
		c.Writer.Header().Set("Access-Control-Allow-Headers", "Content-Type, Content-Length, Authorization, Accept, X-Requested-With")
		if c.Request.Method == "OPTIONS" {
			c.AbortWithStatus(204)
			return
		}
		c.Next()
	}
}

package main

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"

	dapr "github.com/dapr/go-sdk/client"
	"github.com/gin-gonic/gin"
	"golang.org/x/time/rate"
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

	r := gin.Default()

	// 2. 跨域处理
	r.Use(CORSMiddleware())

	// 2. 定义抢票接口
	r.POST("/api/buy", func(c *gin.Context) {
		// 这里对应的就是你 Java 里的那个 TicketActorImpl
		actorType := "TicketActor"
		actorId := "Concert-JayChou-2026"
		method := "deductTicket"
		// 简单传个 "1"，Java 接收端直接用 Integer.parseInt() 转一下就行
		data := []byte("1")

		ctx := context.Background()

		req := &dapr.InvokeActorRequest{
			ActorType: actorType,
			ActorID:   actorId,
			Method:    method,
			Data:      data,
		}

		// 3. 调用 Java 端 Actor
		resp, err := daprClient.InvokeActor(ctx, req)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "调用Actor失败: " + err.Error()})
			return
		}

		// 4. 将 Java Actor 返回的结果（true/false）返给前端
		c.JSON(http.StatusOK, gin.H{
			"message": "抢票结果已返回",
			"data":    string(resp.Data),
		})
	})

	// 3. 通用网关逻辑：支持任意 /:appID/*method
	// 例如：POST /ticket-service/api/buy -> 调用 AppID 为 ticket-service 的 api/buy 方法
	r.Any("/:appID/*method", AuthMiddleware(), RateLimitMiddleware(), func(c *gin.Context) {
		appID := c.Param("appID")
		method := strings.TrimPrefix(c.Param("method"), "/")

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

	// 从中间件获取解析出的 userId
	// userId, _ := c.Get("userId")

	// 构建元数据（gRPC Metadata），透传给 Java 端
	// metadata := map[string]string{
	// 	"x-user-id": fmt.Sprintf("%v", userId),
	// }

	content := &dapr.DataContent{
		Data:        body,
		ContentType: c.ContentType(),
	}

	// 核心：动态调用 Dapr Service Invocation
	resp, err := daprClient.InvokeMethodWithContent(
		c.Request.Context(),
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
		token := c.GetHeader("Authorization")
		if token == "" {
			// 这里仅作演示，实际应解析并校验 JWT
			c.Set("userId", "guest_user")
			c.Next()
			return
		}
		// 模拟 JWT 解析出 userId = 123
		c.Set("userId", "123")
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

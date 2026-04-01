package main

import (
	"context"
	"log"
	"net/http"

	"github.com/dapr/go-sdk/client"
	"github.com/gin-gonic/gin"
)

func main() {
	// 1. 初始化 Dapr 客户端
	// 它会自动读取环境变量 DAPR_GRPC_PORT，所以不用手动配
	daprClient, err := client.NewClient()
	if err != nil {
		log.Fatalf("Dapr Client 初始化失败: %v", err)
	}
	defer daprClient.Close()

	r := gin.Default()

	// 2. 定义抢票接口
	r.POST("/api/buy", func(c *gin.Context) {
		// 这里对应的就是你 Java 里的那个 TicketActorImpl
		actorType := "TicketActor"
		actorId := "Concert-JayChou-2026"
		method := "deductTicket"
		// 简单传个 "1"，Java 接收端直接用 Integer.parseInt() 转一下就行
		data := []byte("1")

		ctx := context.Background()

		req := &client.InvokeActorRequest{
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

	// 运行在 8081
	r.Run(":8081")
}

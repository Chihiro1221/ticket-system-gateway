package utils

import (
	"errors"
	"fmt"
	"strings"

	"github.com/golang-jwt/jwt/v5"
)

// 这里的密钥必须和 Java Spring Boot 中生成 JWT 的密钥完全一致
var jwtSecret = []byte("ticket-system-main-backend-jwt-secret-key-2026")

// CustomClaims 定义 JWT 载荷结构，需与 Java 端生成的字段名一一对应（注意 JSON 标签）
type CustomClaims struct {
	UserId   string `json:"userId"` // Java 端存入的 key 如果是 userId，这里对应
	Username string `json:"username"`
	Role     string `json:"role"`
	jwt.RegisteredClaims
}

// ValidateJWT 校验并解析 Token
func ValidateJWT(tokenString string) (*CustomClaims, error) {
	// 1. 处理 Bearer 前缀 (如果是从 Header 直接传进来的话)
	if strings.HasPrefix(tokenString, "Bearer ") {
		tokenString = strings.TrimPrefix(tokenString, "Bearer ")
	}

	// 2. 解析 Token
	token, err := jwt.ParseWithClaims(tokenString, &CustomClaims{}, func(token *jwt.Token) (interface{}, error) {
		// 校验签名算法是否为 HMAC (HS256)
		if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("意外的签名方法: %v", token.Header["alg"])
		}
		return jwtSecret, nil
	})

	if err != nil {
		if errors.Is(err, jwt.ErrTokenExpired) {
			return nil, errors.New("token 已过期")
		}
		return nil, errors.New("无效的 token: " + err.Error())
	}

	// 3. 转换并返回 Claims
	if claims, ok := token.Claims.(*CustomClaims); ok && token.Valid {
		return claims, nil
	}

	return nil, errors.New("解析 claims 失败")
}

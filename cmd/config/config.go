package config

import (
	"log"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/spf13/viper"
)

type Rule struct {
	Path    string   `mapstructure:"path"`
	Methods []string `mapstructure:"methods"`
	Auth    string   `mapstructure:"auth"`
}

type Route struct {
	AppID string `mapstructure:"app_id"`
	Rules []Rule `mapstructure:"rules"`
}

type Config struct {
	DefaultAuth string  `mapstructure:"default_auth"`
	Routes      []Route `mapstructure:"routes"`
}

var GlobalConfig Config

func InitConfig() {
	_, currentFile, _, _ := runtime.Caller(0)
	configDir := filepath.Join(filepath.Dir(currentFile), "..", "..", "config")
	viper.AddConfigPath(configDir)
	viper.SetConfigType("yml")
	viper.SetConfigName("application")
	if err := viper.ReadInConfig(); err != nil {
		panic("加载网关配置失败: " + err.Error())
	}
	if err := viper.UnmarshalKey("gateway", &GlobalConfig); err != nil {
		panic("解析配置失败: " + err.Error())
	}

	log.Printf("网关配置: %v", GlobalConfig)
}

// 核心匹配算法
func GetRequiredAuth(appID, requestPath, requestMethod string) string {
	for _, route := range GlobalConfig.Routes {
		if route.AppID == appID {
			for _, rule := range route.Rules {
				// 1. 先校验路径是否匹配
				if matchPath(rule.Path, requestPath) {
					// 2. 路径匹配后，再校验 HTTP 方法是否匹配
					if matchMethod(rule.Methods, requestMethod) {
						return rule.Auth
					}
				}
			}
		}
	}
	return GlobalConfig.DefaultAuth
}

// 简单的路径匹配逻辑（支持 /admin/* 这种简单通配符）
func matchPath(configPath, requestPath string) bool {
	if configPath == requestPath {
		return true
	}
	if strings.HasSuffix(configPath, "/**") {
		prefix := strings.TrimSuffix(configPath, "**")
		return strings.HasPrefix(requestPath, prefix)
	}
	if strings.HasSuffix(configPath, "/*") {
		prefix := strings.TrimSuffix(configPath, "*")
		return strings.HasPrefix(requestPath, prefix)
	}
	return false
}

// 校验方法是否在配置的列表中
func matchMethod(configMethods []string, requestMethod string) bool {
	if len(configMethods) == 0 {
		return true
	}
	for _, m := range configMethods {
		if m == "*" || strings.ToUpper(m) == strings.ToUpper(requestMethod) {
			return true
		}
	}
	return false
}

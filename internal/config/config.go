// Package config — 核心运行配置：环境变量 > 默认值。
package config

import (
	"fmt"
	"os"

	"github.com/Sndeok/ClawProxyHub-Next/internal/setting"
)

// Config 是核心进程的运行配置。
type Config struct {
	// HTTP 监听地址
	Addr string
	// 数据目录（SQLite / 插件目录都在其下）
	DataDir string
	// 数据库 DSN；空则用 <DataDir>/cph.db
	DatabaseDSN string
	// 插件安装目录；空则用 <DataDir>/plugins
	PluginDir string
	// 插件市场索引 URL（仪表盘「设置」里的自建地址优先于此项）
	MarketplaceURL string
	// 插件市场出站代理（socks5://host:port / http://host:port；空 = 直连）
	// 仅作为首次启动的默认值，之后以仪表盘「设置」为准
	MarketProxy string
}

// Load 从环境变量读取配置，未设置项用默认值填充。
func Load() *Config {
	cfg := &Config{
		Addr:           envStr("CPH_ADDR", ":8080"),
		DataDir:        envStr("CPH_DATA_DIR", "./data"),
		MarketplaceURL: envStr("CPH_MARKETPLACE_URL", setting.DefaultMarketplaceURL),
		MarketProxy:    envStr("CPH_MARKET_PROXY", ""),
	}
	cfg.DatabaseDSN = envStr("CPH_DATABASE_DSN", cfg.DataDir+"/cph.db")
	cfg.PluginDir = envStr("CPH_PLUGIN_DIR", cfg.DataDir+"/plugins")
	return cfg
}

func envStr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

// Validate 检查配置合法性。
func (c *Config) Validate() error {
	if c.DataDir == "" {
		return fmt.Errorf("data dir must not be empty")
	}
	return nil
}

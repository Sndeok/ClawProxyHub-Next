// cph — ClawProxyHub 核心进程入口。
package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"

	"google.golang.org/protobuf/encoding/protojson"

	accountpkg "github.com/Sndeok/ClawProxyHub-Next/internal/account"
	"github.com/Sndeok/ClawProxyHub-Next/internal/admin"
	"github.com/Sndeok/ClawProxyHub-Next/internal/config"
	"github.com/Sndeok/ClawProxyHub-Next/internal/database"
	"github.com/Sndeok/ClawProxyHub-Next/internal/event"
	"github.com/Sndeok/ClawProxyHub-Next/internal/gateway"
	"github.com/Sndeok/ClawProxyHub-Next/internal/model"
	"github.com/Sndeok/ClawProxyHub-Next/internal/plugin"
	"github.com/Sndeok/ClawProxyHub-Next/internal/router"
	"github.com/Sndeok/ClawProxyHub-Next/internal/setting"
	"github.com/Sndeok/ClawProxyHub-Next/internal/task"
	"github.com/Sndeok/ClawProxyHub-Next/web"
	"gorm.io/gorm"
)

// seedAPIKey 首次部署引导：环境变量指定 key，不存在则入库（加密存储）。
func seedAPIKey(db *gorm.DB, raw string, dataDir string) error {
	sum := sha256.Sum256([]byte(raw))
	hash := hex.EncodeToString(sum[:])
	var count int64
	db.Model(&model.Key{}).Where("key_hash = ?", hash).Count(&count)
	if count > 0 {
		return nil
	}
	return db.Create(&model.Key{
		KeyCipher: string(accountpkg.EncryptCredential(dataDir, []byte(raw))),
		KeyHash:   hash,
		Name:      "seed",
	}).Error
}

// syncPluginRecords 启动插件后同步 plugins 表（安装流程落地前的兜底）。
func syncPluginRecords(db *gorm.DB, plugins *plugin.Manager) {
	for _, name := range plugins.Names() {
		inst, ok := plugins.Get(name)
		if !ok {
			continue
		}
		m := inst.Manifest
		manifestJSON, _ := protojson.Marshal(m)
		var rec model.Plugin
		err := db.Where("name = ?", m.Name).First(&rec).Error
		if err != nil {
			db.Create(&model.Plugin{
				Name: m.Name, Version: m.Version, Author: m.Author,
				ProtocolVersion: m.ProtocolVersion, ManifestJSON: string(manifestJSON),
				Enabled: true,
			})
		} else {
			db.Model(&rec).Updates(map[string]interface{}{
				"version": m.Version, "author": m.Author,
				"protocol_version": m.ProtocolVersion, "manifest_json": string(manifestJSON),
			})
		}
	}
}

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "fatal:", err)
		os.Exit(1)
	}
}

func run() error {
	cfg := config.Load()
	if err := cfg.Validate(); err != nil {
		return err
	}

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	if err := os.MkdirAll(cfg.PluginDir, 0o755); err != nil {
		return fmt.Errorf("create plugin dir: %w", err)
	}

	// 恢复暂存换入必须在打开库之前：换文件 + 清理 -wal/-shm 残留
	if err := database.ApplyPendingRestore(cfg.DatabaseDSN, cfg.DataDir); err != nil {
		return fmt.Errorf("apply pending restore: %w", err)
	}

	db, err := database.Open(ctx, cfg.DatabaseDSN)
	if err != nil {
		return err
	}

	if key := os.Getenv("CPH_SEED_API_KEY"); key != "" {
		if err := seedAPIKey(db, key, cfg.DataDir); err != nil {
			return err
		}
	}

	plugins := plugin.NewManager(cfg.PluginDir, db)
	if bins, err := plugins.Scan(); err == nil {
		for _, bin := range bins {
			inst, err := plugins.Start(ctx, bin)
			if err != nil {
				fmt.Printf("[plugin] start failed: %v\n", err)
				continue
			}
			// 尊重持久化的停用状态：管理页「停止」写 plugins.enabled=0，
			// 重启（含容器重建）后该插件保持停用，直到在管理页点「启动」。
			var rec model.Plugin
			if err := db.Where("name = ?", inst.Name).First(&rec).Error; err == nil && !rec.Enabled {
				plugins.Stop(inst.Name)
				fmt.Printf("[plugin] %s 已停用（plugins.enabled=0），跳过启动\n", inst.Name)
			}
		}
	}
	plugins.RefreshCatalog(ctx)
	syncPluginRecords(db, plugins)
	defer plugins.StopAll()

	// 进程内事件总线：任务执行成功后通知账号服务刷新积分等派生状态
	bus := event.New()
	accounts := accountpkg.New(db, cfg.DataDir, plugins)
	accounts.SubscribeRefresh(ctx, bus)

	engine := task.NewEngine(db, cfg.DataDir, task.NewPluginRunner(plugins), bus)
	engine.Start(ctx)
	defer engine.Stop()

	settings := setting.New(db)
	// 调用日志保留策略（settings.logs.retention_days，0 = 永久保留）
	admin.StartLogRetention(ctx, db, settings)
	// 路由解析器（会话粘性策略来自设置，改动即时生效）
	rt := router.New(db)
	rt.SetStickyPolicy(settings.StickyTTL(), settings.StickyCleanPeriod())
	rt.SetDefaultStrategy(settings.RouteDefaultStrategy())
	rt.StartJanitor(ctx)
	gw := gateway.New(db, cfg.DataDir, plugins, rt, accounts, settings)
	adminSrv := admin.New(db, accounts, plugins, engine, settings, cfg.MarketplaceURL, cfg.MarketProxy, rt, cfg.DataDir, cfg.DatabaseDSN)

	mux := http.NewServeMux()
	mux.Handle("/v1/", gw.Handler())
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"status":"ok"}`))
	})
	mux.Handle("/admin/", adminSrv.Handler())
	// 插件图标（img 标签带不了 Authorization，走免鉴权只读静态服务）
	mux.HandleFunc("GET /assets/plugins/{name}/icon", func(w http.ResponseWriter, r *http.Request) {
		path, ok := plugins.IconFile(r.PathValue("name"))
		if !ok {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Cache-Control", "public, max-age=3600")
		http.ServeFile(w, r, path)
	})
	mux.Handle("/", web.Handler())

	fmt.Printf("listening on %s\n", cfg.Addr)
	httpSrv := &http.Server{Addr: cfg.Addr, Handler: mux}
	errCh := make(chan error, 1)
	go func() { errCh <- httpSrv.ListenAndServe() }()
	select {
	case <-ctx.Done():
		return httpSrv.Shutdown(context.Background())
	case err := <-errCh:
		return err
	}
}

package app

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"
	"github.com/liuran001/MusicBot-Go/bot/config"
	"github.com/liuran001/MusicBot-Go/bot/db"
	"github.com/liuran001/MusicBot-Go/bot/download"
	"github.com/liuran001/MusicBot-Go/bot/id3"
	logpkg "github.com/liuran001/MusicBot-Go/bot/logger"
	"github.com/liuran001/MusicBot-Go/bot/platform"
	"github.com/liuran001/MusicBot-Go/bot/telegram"
	"github.com/liuran001/MusicBot-Go/bot/telegram/handler"
	"github.com/liuran001/MusicBot-Go/bot/worker"
	"github.com/liuran001/MusicBot-Go/plugins/netease"
	gormlogger "gorm.io/gorm/logger"
)

// App wires all application dependencies.
type App struct {
	Config           *config.Config
	Logger           *logpkg.Logger
	DB               *db.Repository
	Pool             *worker.Pool
	Netease          *netease.Client
	PlatformManager  platform.Manager
	Telegram         *telegram.Bot
	RecognizeService *netease.RecognizeService
	Build            BuildInfo
}

// BuildInfo provides build-time metadata.
type BuildInfo struct {
	RuntimeVer string
	BinVersion string
	CommitSHA  string
	BuildTime  string
	BuildArch  string
}

// New builds the application container.
func New(ctx context.Context, configPath string, build BuildInfo) (*App, error) {
	conf, err := config.Load(configPath)
	if err != nil {
		return nil, err
	}

	log, err := logpkg.New(conf.GetString("LogLevel"))
	if err != nil {
		return nil, err
	}

	gormLogger := logpkg.NewGormLogger(log.Slog(), mapLogLevel(conf.GetString("LogLevel")))
	databasePath := conf.GetString("Database")
	if strings.TrimSpace(databasePath) == "" {
		databasePath = "cache.db"
	}

	repo, err := db.NewSQLiteRepository(databasePath, gormLogger)
	if err != nil {
		return nil, fmt.Errorf("init db: %w", err)
	}
	repo.SetDefaults("netease", conf.GetString("DefaultQuality"))

	poolSize := conf.GetInt("WorkerPoolSize")
	pool := worker.New(poolSize)

	musicU := conf.GetPluginString("netease", "music_u")
	if musicU == "" {
		musicU = conf.GetString("MUSIC_U")
	}
	neteaseClient := netease.New(musicU, log)

	platformManager := platform.NewManager()
	neteasePlatformInstance := netease.NewPlatform(neteaseClient)
	platformManager.Register(neteasePlatformInstance)

	tele, err := telegram.New(conf, log)
	if err != nil {
		return nil, fmt.Errorf("init telegram: %w", err)
	}

	recognizeService := netease.NewRecognizeService(conf.GetInt("RecognizePort"))

	return &App{
		Config:           conf,
		Logger:           log,
		DB:               repo,
		Pool:             pool,
		Netease:          neteaseClient,
		PlatformManager:  platformManager,
		Telegram:         tele,
		RecognizeService: recognizeService,
		Build:            build,
	}, nil
}

// Start initializes background services. Telegram startup is added in later waves.
func (a *App) Start(ctx context.Context) error {
	// Start recognition service first
	if a.RecognizeService != nil {
		if err := a.RecognizeService.Start(ctx); err != nil {
			if a.Logger != nil {
				a.Logger.Warn("failed to start recognition service", "error", err)
			}
			// Don't fail app startup if recognition service fails
		} else {
			if a.Logger != nil {
				a.Logger.Info("audio recognition service started successfully")
			}
		}
	}

	meCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	me, err := a.Telegram.GetMe(meCtx)
	if err != nil {
		if a.Logger != nil {
			a.Logger.Error("getMe failed", "error", err)
		}
	}
	botName := ""
	if me != nil {
		botName = me.Username
	}

	cacheDir := strings.TrimSpace(a.Config.GetString("CacheDir"))
	if cacheDir == "" {
		cacheDir = "./cache"
	}

	downloadService := download.NewDownloadService(download.DownloadServiceOptions{
		Timeout:              time.Duration(a.Config.GetInt("DownloadTimeout")) * time.Second,
		ReverseProxy:         a.Config.GetString("ReverseProxy"),
		CheckMD5:             a.Config.GetBool("CheckMD5"),
		EnableMultipart:      a.Config.GetBool("EnableMultipartDownload"),
		MultipartConcurrency: a.Config.GetInt("MultipartConcurrency"),
		MultipartMinSize:     int64(a.Config.GetInt("MultipartMinSizeMB")) * 1024 * 1024,
	})
	id3Service := id3.NewID3Service(a.Logger)

	tagProviders := map[string]id3.ID3TagProvider{}
	if a.Netease != nil {
		tagProviders["netease"] = netease.NewID3Provider(a.Netease)
	}

	rateLimitPerSecond := a.Config.GetFloat64("RateLimitPerSecond")
	if rateLimitPerSecond <= 0 {
		rateLimitPerSecond = 1.0
	}
	rateLimitBurst := a.Config.GetInt("RateLimitBurst")
	if rateLimitBurst <= 0 {
		rateLimitBurst = 3
	}
	rateLimiter := telegram.NewRateLimiter(rateLimitPerSecond, rateLimitBurst)
	rateLimiter.SetLogger(a.Logger)

	downloadConcurrency := a.Config.GetInt("DownloadConcurrency")
	var downloadLimiter chan struct{}
	if downloadConcurrency > 0 {
		downloadLimiter = make(chan struct{}, downloadConcurrency)
	}
	uploadConcurrency := a.Config.GetInt("UploadConcurrency")
	var uploadLimiter chan struct{}
	if uploadConcurrency > 0 {
		uploadLimiter = make(chan struct{}, uploadConcurrency)
	}
	uploadQueueSize := a.Config.GetInt("UploadQueueSize")

	musicHandler := &handler.MusicHandler{
		Repo:            a.DB,
		Pool:            a.Pool,
		Logger:          a.Logger,
		CacheDir:        cacheDir,
		BotName:         botName,
		PlatformManager: a.PlatformManager,
		DownloadService: downloadService,
		ID3Service:      id3Service,
		TagProviders:    tagProviders,
		Limiter:         downloadLimiter,
		UploadLimiter:   uploadLimiter,
		UploadQueueSize: uploadQueueSize,
		UploadBot:       a.Telegram.UploadClient(),
		RateLimiter:     rateLimiter,
	}
	musicHandler.StartWorker(ctx)

	settingsHandler := &handler.SettingsHandler{
		Repo:            a.DB,
		PlatformManager: a.PlatformManager,
		RateLimiter:     rateLimiter,
	}

	router := &handler.Router{
		Music:            musicHandler,
		Search:           &handler.SearchHandler{PlatformManager: a.PlatformManager, Repo: a.DB, RateLimiter: rateLimiter},
		Lyric:            &handler.LyricHandler{PlatformManager: a.PlatformManager, RateLimiter: rateLimiter},
		Recognize:        &handler.RecognizeHandler{CacheDir: cacheDir, Music: musicHandler, RateLimiter: rateLimiter, RecognizeService: a.RecognizeService, Logger: a.Logger},
		About:            &handler.AboutHandler{RuntimeVer: a.Build.RuntimeVer, BinVersion: a.Build.BinVersion, CommitSHA: a.Build.CommitSHA, BuildTime: a.Build.BuildTime, BuildArch: a.Build.BuildArch, RateLimiter: rateLimiter},
		Status:           &handler.StatusHandler{Repo: a.DB, PlatformManager: a.PlatformManager, RateLimiter: rateLimiter},
		Settings:         settingsHandler,
		RmCache:          &handler.RmCacheHandler{Repo: a.DB, PlatformManager: a.PlatformManager, RateLimiter: rateLimiter},
		Callback:         &handler.CallbackMusicHandler{Music: musicHandler, BotName: botName, RateLimiter: rateLimiter},
		SettingsCallback: &handler.SettingsCallbackHandler{Repo: a.DB, PlatformManager: a.PlatformManager, SettingsHandler: settingsHandler, RateLimiter: rateLimiter},
		Inline:           &handler.InlineSearchHandler{Repo: a.DB, PlatformManager: a.PlatformManager, BotName: botName},
		PlatformManager:  a.PlatformManager,
	}

	router.Register(a.Telegram.Client(), botName)

	commands := []models.BotCommand{
		{Command: "start", Description: "开始使用 / 下载音乐"},
		{Command: "search", Description: "搜索音乐"},
		{Command: "settings", Description: "设置默认平台和音质"},
		{Command: "lyric", Description: "获取歌词"},
		{Command: "recognize", Description: "识别语音中的歌曲"},
		{Command: "status", Description: "查看统计信息"},
		{Command: "about", Description: "关于本 Bot"},
	}
	_, _ = a.Telegram.Client().SetMyCommands(ctx, &bot.SetMyCommandsParams{
		Commands: commands,
	})

	go a.Telegram.Start(ctx)
	return nil
}

// Shutdown releases resources.
func (a *App) Shutdown(ctx context.Context) error {
	var firstErr error

	if a.RecognizeService != nil {
		if err := a.RecognizeService.Stop(); err != nil {
			if a.Logger != nil {
				a.Logger.Error("failed to stop recognition service", "error", err)
			}
			if firstErr == nil {
				firstErr = fmt.Errorf("stop recognition service: %w", err)
			}
		}
	}

	if a.Pool != nil {
		if err := a.Pool.Shutdown(ctx); err != nil {
			a.Pool.StopNow()
			if firstErr == nil {
				firstErr = fmt.Errorf("shutdown worker pool: %w", err)
			}
		}
	}

	if a.DB != nil {
		if err := a.DB.Close(); err != nil {
			if a.Logger != nil {
				a.Logger.Error("failed to close database", "error", err)
			}
			if firstErr == nil {
				firstErr = fmt.Errorf("close database: %w", err)
			}
		}
	}

	if a.Logger != nil {
		if err := a.Logger.Close(); err != nil {
			if firstErr == nil {
				firstErr = fmt.Errorf("close logger: %w", err)
			}
		}
	}

	return firstErr
}

func mapLogLevel(level string) gormlogger.LogLevel {
	switch strings.ToLower(strings.TrimSpace(level)) {
	case "debug", "trace":
		return gormlogger.Info
	case "warn", "warning":
		return gormlogger.Warn
	case "error", "fatal", "panic":
		return gormlogger.Error
	case "info":
		fallthrough
	default:
		return gormlogger.Info
	}
}

package main

import (
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"

	"github.com/litai686/trojan-go/desktop/frontend"
	"github.com/litai686/trojan-go/desktop/internal/paths"
	"github.com/litai686/trojan-go/desktop/internal/store"
	"github.com/litai686/trojan-go/desktop/internal/trojan"
	"github.com/litai686/trojan-go/desktop/internal/update"
	"github.com/wailsapp/wails/v3/pkg/application"
	"github.com/wailsapp/wails/v3/pkg/updater"
)

func main() {
	if wantsVersion(os.Args[1:]) {
		fmt.Printf("Trojan-Go Desktop %s (commit %s, built %s)\n", version, commit, buildTime)
		return
	}

	// The frontend is embedded and has no build step. `wails3 dev` still
	// exports FRONTEND_DEVSERVER_URL (it assumes a Vite server); ignore it so
	// the embedded assets are served instead of waiting for a server we never
	// start.
	if os.Getenv("TROJAN_DESKTOP_EMBEDDED_FRONTEND") == "1" {
		_ = os.Unsetenv("FRONTEND_DEVSERVER_URL")
	}

	dataDir, err := paths.DataDir()
	if err != nil {
		fmt.Fprintln(os.Stderr, "无法确定数据目录:", err)
		os.Exit(1)
	}
	installDir, _ := paths.InstallDir()

	logFile := openLogFile(dataDir)
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	if logFile != nil {
		logger = slog.New(slog.NewTextHandler(logFile, &slog.HandlerOptions{Level: slog.LevelInfo}))
	}

	app := application.New(application.Options{
		Name:        "Trojan-Go Desktop",
		Description: "Trojan-Go 桌面客户端",
		Logger:      logger,
		LogLevel:    slog.LevelInfo,
		Assets: application.AssetOptions{
			Handler: application.AssetFileServerFS(frontend.Assets),
		},
		Mac: application.MacOptions{
			ApplicationShouldTerminateAfterLastWindowClosed: false,
		},
		Windows: application.WindowsOptions{
			DisableQuitOnLastWindowClosed: true,
		},
		Linux: application.LinuxOptions{
			DisableQuitOnLastWindowClosed: true,
		},
		SingleInstance: &application.SingleInstanceOptions{
			UniqueID: "com.litai686.trojan-go-desktop",
			ExitCode: 0,
		},
	})

	st, err := store.Open(dataDir)
	if err != nil {
		fatal(logger, "打开配置存储失败", err)
	}
	for _, warning := range st.Warnings() {
		logger.Warn(warning)
	}

	hub := trojan.NewLogHub(func(name string, data any) {
		app.Event.Emit(name, data)
	}, st.GetSettings().MaxLogLines)
	hub.SetSink(func(entry store.LogEntry) {
		logger.Info("trojan-go",
			"level", entry.Level,
			"stream", entry.Stream,
			"text", entry.Text,
		)
	})
	hub.Start()

	manager := trojan.NewManager(dataDir, st.GetSettings, hub, func(state store.RuntimeState) {
		app.Event.Emit("runtime:state", state)
	})

	provider := update.NewProvider(update.Config{
		Repository: st.GetSettings().UpdateRepository,
		TagPrefix:  "desktop-v",
		Prerelease: st.GetSettings().UpdateChannel == "prerelease",
		ProxyURL:   manager.ProxyURL,
	})

	profileService := &ProfileService{store: st, manager: manager, app: app}
	runtimeService := &RuntimeService{store: st, manager: manager, hub: hub, app: app}
	settingsService := &SettingsService{store: st, hub: hub, provider: provider, app: app}
	updateService := &UpdateService{
		app:        app,
		store:      st,
		manager:    manager,
		provider:   provider,
		dataDir:    dataDir,
		installDir: installDir,
	}
	systemService := &SystemService{
		app:        app,
		store:      st,
		manager:    manager,
		hub:        hub,
		dataDir:    dataDir,
		installDir: installDir,
	}

	app.RegisterService(application.NewService(profileService))
	app.RegisterService(application.NewService(runtimeService))
	app.RegisterService(application.NewService(settingsService))
	app.RegisterService(application.NewService(updateService))
	app.RegisterService(application.NewService(systemService))

	if version != "" {
		if err := app.Updater.Init(updater.Config{
			CurrentVersion: version,
			Providers:      []updater.Provider{provider},
		}); err != nil {
			hub.AddSystem("初始化更新模块失败: " + err.Error())
		}
	}

	window := app.Window.NewWithOptions(application.WebviewWindowOptions{
		Title:               "Trojan-Go Desktop",
		Width:               1120,
		Height:              760,
		MinWidth:            900,
		MinHeight:           600,
		MaximiseButtonState: application.ButtonDisabled,
		BackgroundColour:    application.NewRGB(15, 18, 26),
		URL:                 "/",
	})
	installTray(app, window)

	app.OnShutdown(func() {
		manager.Shutdown()
		hub.Stop()
		if logFile != nil {
			_ = logFile.Close()
		}
	})

	if err := app.Run(); err != nil {
		logger.Error("应用退出", "error", err)
	}
}

func wantsVersion(args []string) bool {
	for _, arg := range args {
		if arg == "-version" || arg == "--version" || arg == "version" {
			return true
		}
	}
	return false
}

func openLogFile(dataDir string) *os.File {
	if err := os.MkdirAll(dataDir, 0o700); err != nil {
		return nil
	}
	f, err := os.OpenFile(filepath.Join(dataDir, "desktop.log"), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return nil
	}
	return f
}

func fatal(logger *slog.Logger, message string, err error) {
	logger.Error(message, "error", err)
	fmt.Fprintln(os.Stderr, message+":", err)
	os.Exit(1)
}

package store

import "time"

// Profile is one saved trojan-go client configuration.
type Profile struct {
	ID        string        `json:"id"`
	Name      string        `json:"name"`
	Config    ProfileConfig `json:"config"`
	CreatedAt time.Time     `json:"createdAt"`
	UpdatedAt time.Time     `json:"updatedAt"`
}

// ProfileConfig holds the subset of trojan-go client options exposed by the UI.
type ProfileConfig struct {
	ServerAddr string `json:"serverAddr"`
	ServerPort int    `json:"serverPort"`
	Password   string `json:"password"`
	SNI        string `json:"sni"`
	LocalAddr  string `json:"localAddr"`
	LocalPort  int    `json:"localPort"`
	Mux        bool   `json:"mux"`
	Router     bool   `json:"router"`
	VerifyTLS  bool   `json:"verifyTls"`
	LogLevel   int    `json:"logLevel"`
}

// Settings are global desktop application settings.
type Settings struct {
	AutoStart        bool   `json:"autoStart"`
	AutoCheckUpdate  bool   `json:"autoCheckUpdate"`
	MaxLogLines      int    `json:"maxLogLines"`
	Theme            string `json:"theme"`
	BinaryPath       string `json:"binaryPath"`
	LastProfileID    string `json:"lastProfileId"`
	UpdateChannel    string `json:"updateChannel"`
	UpdateRepository string `json:"updateRepository"`
}

// RuntimeState describes the managed trojan-go process.
type RuntimeState struct {
	Status      string `json:"status"`
	ProfileID   string `json:"profileId"`
	ProfileName string `json:"profileName"`
	Pid         int    `json:"pid"`
	StartedAt   int64  `json:"startedAt"`
	LocalAddr   string `json:"localAddr"`
	ServerAddr  string `json:"serverAddr"`
	BinaryPath  string `json:"binaryPath"`
	LastError   string `json:"lastError"`
}

// LogEntry is one line of captured trojan-go output.
type LogEntry struct {
	Seq    int64  `json:"seq"`
	Time   int64  `json:"time"`
	Stream string `json:"stream"`
	Level  string `json:"level"`
	Text   string `json:"text"`
}

// VersionInfo is displayed on the settings/about page.
type VersionInfo struct {
	AppVersion    string `json:"appVersion"`
	TrojanVersion string `json:"trojanVersion"`
	Commit        string `json:"commit"`
	BuildTime     string `json:"buildTime"`
	GoVersion     string `json:"goVersion"`
	Platform      string `json:"platform"`
	WailsVersion  string `json:"wailsVersion"`
	DataDir       string `json:"dataDir"`
	InstallDir    string `json:"installDir"`
	BinaryPath    string `json:"binaryPath"`
}

// UpdateInfo is the frontend-facing summary of an update check.
type UpdateInfo struct {
	CurrentVersion string `json:"currentVersion"`
	LatestVersion  string `json:"latestVersion"`
	Available      bool   `json:"available"`
	Name           string `json:"name"`
	Notes          string `json:"notes"`
	PublishedAt    string `json:"publishedAt"`
	AssetName      string `json:"assetName"`
	AssetSize      int64  `json:"assetSize"`
	HTMLURL        string `json:"htmlUrl"`
}

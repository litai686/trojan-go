package trojan

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"net"
	"os/exec"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	"github.com/litai686/trojan-go/desktop/internal/store"
)

const (
	stopGracePeriod   = 5 * time.Second
	killGracePeriod   = 2 * time.Second
	proxyProbeTimeout = 300 * time.Millisecond
)

type running struct {
	cmd      *exec.Cmd
	profile  store.Profile
	stopping atomic.Bool
	done     chan struct{}
}

// Manager owns the single trojan-go child process.
type Manager struct {
	opMu sync.Mutex

	mu      sync.RWMutex
	dataDir string
	state   store.RuntimeState
	current *running

	settings func() store.Settings
	hub      *LogHub
	onState  func(store.RuntimeState)
}

// NewManager creates a process manager.
func NewManager(dataDir string, settings func() store.Settings, hub *LogHub, onState func(store.RuntimeState)) *Manager {
	return &Manager{
		dataDir:  dataDir,
		settings: settings,
		hub:      hub,
		onState:  onState,
		state:    store.RuntimeState{Status: "stopped"},
	}
}

// State returns a snapshot of the current state.
func (m *Manager) State() store.RuntimeState {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.state
}

// ProxyURL returns a socks5 URL for the running profile when its local
// listener accepts connections. It returns "" when no profile is running or
// the listener is unreachable, so callers can connect directly instead.
func (m *Manager) ProxyURL() string {
	st := m.State()
	if st.Status != "running" || st.LocalAddr == "" {
		return ""
	}
	host, port, err := net.SplitHostPort(st.LocalAddr)
	if err != nil || port == "" {
		return ""
	}
	switch host {
	case "", "0.0.0.0", "::":
		host = "127.0.0.1"
	}
	addr := net.JoinHostPort(host, port)
	conn, err := net.DialTimeout("tcp", addr, proxyProbeTimeout)
	if err != nil {
		return ""
	}
	_ = conn.Close()
	return "socks5://" + addr
}

// IsActive reports whether profileID is the currently running profile.
func (m *Manager) IsActive(profileID string) bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.current != nil && m.current.profile.ID == profileID
}

// Start stops any current process and starts profile.
func (m *Manager) Start(p store.Profile) (store.RuntimeState, error) {
	m.opMu.Lock()
	defer m.opMu.Unlock()
	m.stopLocked()
	return m.startLocked(p)
}

// Stop terminates the running process.
func (m *Manager) Stop() (store.RuntimeState, error) {
	m.opMu.Lock()
	defer m.opMu.Unlock()
	m.stopLocked()
	return m.State(), nil
}

// Restart restarts the current profile.
func (m *Manager) Restart() (store.RuntimeState, error) {
	m.opMu.Lock()
	defer m.opMu.Unlock()

	m.mu.RLock()
	r := m.current
	m.mu.RUnlock()
	if r == nil {
		return m.State(), errors.New("当前没有运行中的配置")
	}
	profile := r.profile
	m.stopLocked()
	return m.startLocked(profile)
}

// Shutdown stops the child process. It is safe to call multiple times.
func (m *Manager) Shutdown() {
	m.opMu.Lock()
	defer m.opMu.Unlock()
	m.stopLocked()
}

func (m *Manager) startLocked(p store.Profile) (store.RuntimeState, error) {
	cfg := NormalizeProfileConfig(p.Config)
	p.Config = cfg
	if err := ValidateProfile(&p, []store.Profile{p}); err != nil {
		m.setError(err.Error())
		return m.State(), err
	}

	cfgPath, err := WriteRuntimeConfig(m.dataDir, cfg)
	if err != nil {
		m.setError(err.Error())
		return m.State(), err
	}
	bin, err := ResolveBinary(m.settings())
	if err != nil {
		m.setError(err.Error())
		return m.State(), err
	}

	cmd := newCommand(bin, "-config", cfgPath)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		m.setError("创建 stdout 管道失败: " + err.Error())
		return m.State(), err
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		m.setError("创建 stderr 管道失败: " + err.Error())
		return m.State(), err
	}

	local := net.JoinHostPort(cfg.LocalAddr, strconv.Itoa(cfg.LocalPort))
	m.setStarting(p, bin, local)

	if err := cmd.Start(); err != nil {
		m.setError("启动 trojan-go 失败: " + err.Error())
		return m.State(), err
	}
	if err := afterStart(cmd); err != nil {
		m.hub.AddSystem("无法将 trojan-go 加入作业对象: " + err.Error())
	}

	r := &running{
		cmd:     cmd,
		profile: p,
		done:    make(chan struct{}),
	}
	m.mu.Lock()
	m.current = r
	m.state = store.RuntimeState{
		Status:      "running",
		ProfileID:   p.ID,
		ProfileName: p.Name,
		Pid:         cmd.Process.Pid,
		StartedAt:   time.Now().UnixMilli(),
		LocalAddr:   local,
		ServerAddr:  net.JoinHostPort(cfg.ServerAddr, strconv.Itoa(cfg.ServerPort)),
		BinaryPath:  bin,
	}
	m.mu.Unlock()
	m.emitState()

	m.hub.AddSystem(fmt.Sprintf("已启动 trojan-go (PID %d)，本地监听 %s", cmd.Process.Pid, local))
	go m.readPipe(r, stdout, "stdout")
	go m.readPipe(r, stderr, "stderr")
	go m.wait(r)

	return m.State(), nil
}

func (m *Manager) stopLocked() {
	m.mu.Lock()
	r := m.current
	if r == nil {
		m.mu.Unlock()
		return
	}
	m.current = nil
	r.stopping.Store(true)
	m.state.Status = "stopping"
	m.state.Pid = 0
	m.mu.Unlock()
	m.emitState()

	m.hub.AddSystem("正在停止 trojan-go…")
	_ = terminate(r.cmd)

	select {
	case <-r.done:
	case <-time.After(stopGracePeriod):
		m.hub.AddSystem("trojan-go 未在 5 秒内退出，强制结束")
		_ = forceKill(r.cmd)
		select {
		case <-r.done:
		case <-time.After(killGracePeriod):
		}
	}

	m.mu.Lock()
	m.state.Status = "stopped"
	m.state.Pid = 0
	m.state.StartedAt = 0
	m.state.LastError = ""
	m.mu.Unlock()
	m.emitState()
	m.hub.AddSystem("trojan-go 已停止")
}

func (m *Manager) wait(r *running) {
	err := r.cmd.Wait()
	close(r.done)

	m.mu.Lock()
	isCurrent := m.current == r
	if isCurrent {
		m.current = nil
	}
	m.mu.Unlock()

	if !isCurrent {
		return
	}

	message := "trojan-go 已退出"
	if err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			message = fmt.Sprintf("trojan-go 已退出，退出码 %d", exitErr.ExitCode())
		} else {
			message = "trojan-go 已退出: " + err.Error()
		}
	}

	m.mu.Lock()
	m.state.Status = "error"
	m.state.Pid = 0
	m.state.StartedAt = 0
	m.state.LastError = message
	m.mu.Unlock()
	m.emitState()
	m.hub.AddSystem(message)
}

func (m *Manager) readPipe(r *running, pipe io.ReadCloser, stream string) {
	defer pipe.Close()
	scanner := bufio.NewScanner(pipe)
	scanner.Buffer(make([]byte, 64*1024), 1024*1024)
	for scanner.Scan() {
		m.hub.Add(stream, scanner.Text())
	}
	if err := scanner.Err(); err != nil && !r.stopping.Load() {
		m.hub.AddSystem("读取 " + stream + " 失败: " + err.Error())
	}
}

func (m *Manager) setStarting(p store.Profile, bin, local string) {
	m.mu.Lock()
	m.state = store.RuntimeState{
		Status:      "starting",
		ProfileID:   p.ID,
		ProfileName: p.Name,
		LocalAddr:   local,
		ServerAddr:  net.JoinHostPort(p.Config.ServerAddr, strconv.Itoa(p.Config.ServerPort)),
		BinaryPath:  bin,
	}
	m.mu.Unlock()
	m.emitState()
}

func (m *Manager) setError(message string) {
	m.mu.Lock()
	m.state.Status = "error"
	m.state.Pid = 0
	m.state.StartedAt = 0
	m.state.LastError = message
	m.mu.Unlock()
	m.emitState()
	m.hub.AddSystem(message)
}

func (m *Manager) emitState() {
	if m.onState == nil {
		return
	}
	m.onState(m.State())
}

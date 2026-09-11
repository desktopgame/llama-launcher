// Package measure fills in measured_cost by actually loading each model and
// watching how much memory llama-swap reports.
//
// llama-swap はモデル単位のメモリメトリクスを持たない（internal/perf は
// SysStat と GpuStat のみ）。そこで「アンロード直後の値」と「ロード完了後の
// 値」の差分を取る。ログ行のフォーマットに依存しないので llama.cpp の
// ビルドが変わっても壊れず、mmproj やドラフトモデル、アロケータの
// オーバーヘッドまで込みの実際の総量が取れる。
package measure

import (
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/desktopgame/llama-launcher/internal/profile"
	"github.com/desktopgame/llama-launcher/internal/runtime"
	"github.com/desktopgame/llama-launcher/internal/swap"
	"github.com/desktopgame/llama-launcher/internal/workspace"
)

const (
	// perfInterval is how often llama-swap samples system statistics. 5s is
	// both its default and its enforced minimum ("every must be at least 5s").
	perfInterval = 5 * time.Second
	// pollInterval must not be shorter than perfInterval: /metrics just repeats
	// the last sample in between, so faster polling would read the same value
	// several times and the settle check would see a false calm while memory
	// is still climbing.
	pollInterval = perfInterval
	// settleThresholdMB is the peak-to-peak spread below which memory counts as stable.
	settleThresholdMB = 150
	// settleTimeout caps how long we wait for memory to stop moving.
	settleTimeout = 3 * time.Minute
	// metricsTimeout is how long to wait for the monitor's first sample.
	metricsTimeout = 30 * time.Second
)

// Options tunes a measurement run.
type Options struct {
	Port         int           // port llama-swap listens on during the run
	ApiKeys      []string      // llama-swap apiKeys; when set, our own requests must carry one too
	SettleWindow time.Duration // memory must stay flat for this long
	LoadTimeout  time.Duration // per-model budget for reaching the ready state
	Log          func(format string, args ...any)
}

func (o *Options) fillDefaults() {
	if o.SettleWindow <= 0 {
		// perfInterval おきに4サンプル分
		o.SettleWindow = 15 * time.Second
	}
	if o.LoadTimeout <= 0 {
		o.LoadTimeout = 10 * time.Minute
	}
	if o.Log == nil {
		o.Log = func(string, ...any) {}
	}
}

// Result is the outcome for one profile.
type Result struct {
	ProfileName string
	BaselineMB  int
	LoadedMB    int
	CostMB      int
	Err         error
}

// Run measures every named profile in turn and writes measured_cost back to
// disk. An empty names slice measures every usable profile.
//
// Models go in the on-demand group so llama-swap keeps at most one loaded at a
// time, and TTL is 0 so nothing unloads underneath the measurement.
func Run(
	names []string,
	profMgr *profile.Manager,
	rtMgr *runtime.Manager,
	opts Options,
) ([]Result, error) {
	opts.fillDefaults()

	if err := swap.CheckInstalled(); err != nil {
		return nil, err
	}
	if err := checkPortFree(opts.Port); err != nil {
		return nil, err
	}

	// 常駐なし・TTL 0 のワークスペースを組む。壊れたプロファイルは警告してスキップされる
	ws, warnings, err := workspace.BuildResident(profMgr, rtMgr, nil, 0)
	if err != nil {
		return nil, err
	}
	for _, w := range warnings {
		opts.Log("Warning: %s", w)
	}

	targets, err := selectTargets(ws, names)
	if err != nil {
		return nil, err
	}
	ws.Entries = targets

	configPath, err := swap.GenerateConfig(ws, profMgr, rtMgr, opts.Port,
		swap.WithPerfInterval(perfInterval), swap.WithAPIKeys(opts.ApiKeys))
	if err != nil {
		return nil, fmt.Errorf("failed to generate config: %w", err)
	}

	proc := &swap.Process{}
	if err := proc.Start(configPath, opts.Port); err != nil {
		return nil, fmt.Errorf("failed to start llama-swap: %w", err)
	}
	defer proc.Stop()
	opts.Log("llama-swap started on port %d (log: %s)", opts.Port, proc.LogPath())

	// 127.0.0.1 を使う。この環境では localhost への新規TCP接続に約2秒かかる
	// apiKeys が設定されていても、llama-swap 側はリストの中のどれか1つで通す
	var apiKey string
	if len(opts.ApiKeys) > 0 {
		apiKey = opts.ApiKeys[0]
	}
	m := &measurer{
		base:   fmt.Sprintf("http://127.0.0.1:%d", opts.Port),
		client: &http.Client{Timeout: 30 * time.Second},
		apiKey: apiKey,
		opts:   opts,
	}
	if err := m.waitHealthy(60 * time.Second); err != nil {
		return nil, err
	}
	// 監視の最初のサンプルが入るまで /metrics は空で返ってくる
	if err := m.waitMetrics(metricsTimeout); err != nil {
		return nil, err
	}

	results := make([]Result, 0, len(targets))
	for i, entry := range targets {
		opts.Log("[%d/%d] measuring %s ...", i+1, len(targets), entry.ProfileName)
		r := m.measureOne(entry.ProfileName)
		if r.Err == nil {
			if err := writeBack(profMgr, entry.ProfileName, r.CostMB); err != nil {
				r.Err = fmt.Errorf("measured %d MB but failed to save: %w", r.CostMB, err)
			}
		}
		if r.Err != nil {
			opts.Log("[%d/%d] %s: %v", i+1, len(targets), entry.ProfileName, r.Err)
		} else {
			opts.Log("[%d/%d] %s: %d MB (%d -> %d)",
				i+1, len(targets), entry.ProfileName, r.CostMB, r.BaselineMB, r.LoadedMB)
		}
		results = append(results, r)
	}

	if err := m.unloadAll(); err != nil {
		opts.Log("Warning: final unload failed: %v", err)
	}
	return results, nil
}

// selectTargets narrows the workspace entries down to the requested names,
// keeping the workspace order. An empty names slice keeps everything.
func selectTargets(ws *workspace.Workspace, names []string) ([]workspace.Entry, error) {
	if len(names) == 0 {
		if len(ws.Entries) == 0 {
			return nil, fmt.Errorf("no usable profiles to measure")
		}
		return ws.Entries, nil
	}

	usable := make(map[string]bool, len(ws.Entries))
	for _, e := range ws.Entries {
		usable[e.ProfileName] = true
	}
	var targets []workspace.Entry
	for _, name := range names {
		// 名前を書いた=意図があるので黙って飛ばさない
		if !usable[name] {
			return nil, fmt.Errorf("profile %q is not usable (missing model file or runtime?)", name)
		}
		targets = append(targets, workspace.Entry{ProfileName: name, Resident: false, TTL: 0})
	}
	return targets, nil
}

func writeBack(profMgr *profile.Manager, name string, costMB int) error {
	prof, err := profMgr.Load(name)
	if err != nil {
		return err
	}
	// ファイル名がプロファイルの同一性なので、Save 先がずれないよう合わせる
	prof.Name = name
	prof.MeasuredCost = &costMB
	prof.MeasuredAt = time.Now().Format(time.RFC3339)
	return profMgr.Save(prof)
}

func checkPortFree(port int) error {
	ln, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", port))
	if err != nil {
		return fmt.Errorf("port %d is already in use; stop the running llama-swap first "+
			"(a measurement is only meaningful with exclusive use of memory)", port)
	}
	return ln.Close()
}

type measurer struct {
	base   string
	client *http.Client
	apiKey string
	opts   Options
}

// authHeader sets the Bearer token llama-swap's apiKeyAuth middleware expects,
// covering inference, management, and metrics routes alike. No-op when apiKeys
// isn't configured.
func (m *measurer) authHeader(req *http.Request) {
	if m.apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+m.apiKey)
	}
}

func (m *measurer) doGet(client *http.Client, url string) (*http.Response, error) {
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	m.authHeader(req)
	return client.Do(req)
}

func (m *measurer) doPost(url, contentType string) (*http.Response, error) {
	req, err := http.NewRequest(http.MethodPost, url, nil)
	if err != nil {
		return nil, err
	}
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	m.authHeader(req)
	return m.client.Do(req)
}

func (m *measurer) measureOne(name string) Result {
	r := Result{ProfileName: name}

	if err := m.unloadAll(); err != nil {
		r.Err = err
		return r
	}
	baseline, err := m.settledMemoryMB()
	if err != nil {
		r.Err = fmt.Errorf("baseline: %w", err)
		return r
	}
	r.BaselineMB = baseline

	if err := m.load(name); err != nil {
		r.Err = err
		return r
	}
	loaded, err := m.settledMemoryMB()
	if err != nil {
		r.Err = fmt.Errorf("after load: %w", err)
		return r
	}
	r.LoadedMB = loaded

	r.CostMB = loaded - baseline
	if r.CostMB < 0 {
		// 他のプロセスが同時にメモリを解放すると負になりうる
		r.CostMB = 0
	}
	return r
}

// load forces llama-swap to start the model the same way its own preload hook
// does: a plain GET at the model's upstream root.
func (m *measurer) load(name string) error {
	client := &http.Client{Timeout: m.opts.LoadTimeout}
	resp, err := m.doGet(client, m.base+"/upstream/"+name+"/")
	if err != nil {
		return fmt.Errorf("load request failed: %w", err)
	}
	io.Copy(io.Discard, resp.Body)
	resp.Body.Close()

	deadline := time.Now().Add(m.opts.LoadTimeout)
	for time.Now().Before(deadline) {
		state, err := m.modelState(name)
		if err != nil {
			return err
		}
		if state == "ready" {
			return nil
		}
		time.Sleep(pollInterval)
	}
	return fmt.Errorf("model did not become ready within %s", m.opts.LoadTimeout)
}

func (m *measurer) unloadAll() error {
	resp, err := m.doPost(m.base+"/api/models/unload", "application/json")
	if err != nil {
		return fmt.Errorf("unload request failed: %w", err)
	}
	io.Copy(io.Discard, resp.Body)
	resp.Body.Close()

	deadline := time.Now().Add(60 * time.Second)
	for time.Now().Before(deadline) {
		running, err := m.runningModels()
		if err != nil {
			return err
		}
		if len(running) == 0 {
			return nil
		}
		time.Sleep(pollInterval)
	}
	return fmt.Errorf("models were still loaded after unload")
}

// settledMemoryMB samples used memory until it stops moving, so a reading is
// never taken while a model is still faulting pages in.
func (m *measurer) settledMemoryMB() (int, error) {
	windowLen := int(m.opts.SettleWindow/pollInterval) + 1
	if windowLen < 2 {
		windowLen = 2
	}

	var window []int
	deadline := time.Now().Add(settleTimeout)
	for {
		mb, err := m.memoryUsedMB()
		if err != nil {
			return 0, err
		}
		window = append(window, mb)
		if len(window) > windowLen {
			window = window[1:]
		}
		if len(window) == windowLen && spread(window) < settleThresholdMB {
			return window[len(window)-1], nil
		}
		if time.Now().After(deadline) {
			m.opts.Log("Warning: memory did not settle within %s; using the last sample", settleTimeout)
			return mb, nil
		}
		time.Sleep(pollInterval)
	}
}

func spread(xs []int) int {
	lo, hi := xs[0], xs[0]
	for _, x := range xs {
		if x < lo {
			lo = x
		}
		if x > hi {
			hi = x
		}
	}
	return hi - lo
}

// memoryUsedMB reads llamaswap_memory_used_bytes out of the Prometheus endpoint.
// GPU側のメトリクスはユニファイドメモリ環境で実態と合わない値を返すので使わない。
func (m *measurer) memoryUsedMB() (int, error) {
	resp, err := m.doGet(m.client, m.base+"/metrics")
	if err != nil {
		return 0, fmt.Errorf("metrics request failed: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return 0, fmt.Errorf("metrics returned status %d (performance monitor unavailable?)", resp.StatusCode)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return 0, err
	}
	return parseMemoryUsedMB(string(body))
}

const memoryUsedMetric = "llamaswap_memory_used_bytes"

func parseMemoryUsedMB(metrics string) (int, error) {
	for _, line := range strings.Split(metrics, "\n") {
		if strings.HasPrefix(line, "#") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) != 2 || fields[0] != memoryUsedMetric {
			continue
		}
		bytes, err := strconv.ParseFloat(fields[1], 64)
		if err != nil {
			return 0, fmt.Errorf("could not parse %s: %w", memoryUsedMetric, err)
		}
		return int(bytes / (1024 * 1024)), nil
	}
	return 0, fmt.Errorf("%s not found in /metrics", memoryUsedMetric)
}

func (m *measurer) modelState(name string) (string, error) {
	running, err := m.runningModels()
	if err != nil {
		return "", err
	}
	return running[name], nil
}

// runningModels returns model id -> state for everything that is not stopped.
func (m *measurer) runningModels() (map[string]string, error) {
	resp, err := m.doGet(m.client, m.base+"/running")
	if err != nil {
		return nil, fmt.Errorf("running request failed: %w", err)
	}
	defer resp.Body.Close()

	var payload struct {
		Running []struct {
			Model string `json:"model"`
			State string `json:"state"`
		} `json:"running"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return nil, fmt.Errorf("could not parse /running: %w", err)
	}

	states := make(map[string]string)
	for _, r := range payload.Running {
		if r.State == "stopped" {
			continue
		}
		states[r.Model] = r.State
	}
	return states, nil
}

func (m *measurer) waitHealthy(timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		resp, err := m.doGet(m.client, m.base+"/health")
		if err == nil {
			io.Copy(io.Discard, resp.Body)
			resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				return nil
			}
		}
		time.Sleep(pollInterval)
	}
	return fmt.Errorf("llama-swap did not become healthy on %s within %s", m.base, timeout)
}

// waitMetrics blocks until the performance monitor has produced its first
// sample. Until then /metrics answers 200 with an empty body.
func (m *measurer) waitMetrics(timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	var last error
	for time.Now().Before(deadline) {
		if _, err := m.memoryUsedMB(); err == nil {
			return nil
		} else {
			last = err
		}
		time.Sleep(pollInterval)
	}
	return fmt.Errorf("no memory metrics after %s (%v); is performance monitoring disabled?", timeout, last)
}

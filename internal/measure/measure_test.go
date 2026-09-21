package measure

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/desktopgame/llama-launcher/internal/workspace"
)

// ロード要求が失敗を返したら、ready を待たずに即座に諦めること。
// そうしないと、起動しないモデルに対して LoadTimeout いっぱい待ち続ける。
func TestLoadFailsFastOnUpstreamError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "upstream command exited prematurely", http.StatusInternalServerError)
	}))
	defer srv.Close()

	m := &measurer{
		base:   srv.URL,
		client: srv.Client(),
		opts:   Options{LoadTimeout: 10 * time.Minute},
	}

	start := time.Now()
	err := m.load("broken")
	if err == nil {
		t.Fatal("expected an error when the upstream fails")
	}
	if !strings.Contains(err.Error(), "HTTP 500") {
		t.Errorf("error = %v, want it to mention HTTP 500", err)
	}
	if elapsed := time.Since(start); elapsed > 5*time.Second {
		t.Errorf("load took %s; it should return as soon as the upstream fails", elapsed)
	}
}

func TestParseMemoryUsedMB(t *testing.T) {
	// 実際の /metrics 出力から抜粋したもの
	metrics := `# HELP llamaswap_memory_total_bytes Total memory in bytes
# TYPE llamaswap_memory_total_bytes gauge
llamaswap_memory_total_bytes 136522498048
# HELP llamaswap_memory_used_bytes Used memory in bytes
# TYPE llamaswap_memory_used_bytes gauge
llamaswap_memory_used_bytes 25288507392
llamaswap_memory_free_bytes 111232942080
`
	got, err := parseMemoryUsedMB(metrics)
	if err != nil {
		t.Fatal(err)
	}
	if want := 25288507392 / (1024 * 1024); got != want {
		t.Errorf("got %d MB, want %d MB", got, want)
	}
}

func TestParseMemoryUsedMBMissing(t *testing.T) {
	if _, err := parseMemoryUsedMB("llamaswap_memory_free_bytes 1\n"); err == nil {
		t.Fatal("expected an error when the metric is absent")
	}
}

// HELP行のような "# ... llamaswap_memory_used_bytes ..." を値と誤読しないこと
func TestParseMemoryUsedMBIgnoresComments(t *testing.T) {
	metrics := `# HELP llamaswap_memory_used_bytes Used memory in bytes
# TYPE llamaswap_memory_used_bytes gauge
llamaswap_memory_used_bytes 2097152
`
	got, err := parseMemoryUsedMB(metrics)
	if err != nil {
		t.Fatal(err)
	}
	if got != 2 {
		t.Errorf("got %d MB, want 2 MB", got)
	}
}

func TestSelectTargets(t *testing.T) {
	ws := &workspace.Workspace{Entries: []workspace.Entry{
		{ProfileName: "a"},
		{ProfileName: "b"},
	}}

	all, err := selectTargets(ws, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 2 {
		t.Errorf("got %d targets, want 2", len(all))
	}

	some, err := selectTargets(ws, []string{"b"})
	if err != nil {
		t.Fatal(err)
	}
	if len(some) != 1 || some[0].ProfileName != "b" {
		t.Errorf("got %v, want just b", some)
	}
	// 測定中に他のモデルが載らないよう、常にon-demand・TTL 0
	if some[0].Resident || some[0].TTL != 0 {
		t.Errorf("got resident=%v ttl=%d, want false/0", some[0].Resident, some[0].TTL)
	}

	// 名前を書いたのに使えない場合は黙って飛ばさない
	if _, err := selectTargets(ws, []string{"gone"}); err == nil {
		t.Fatal("expected an error for an unusable profile")
	}
}

func TestSpread(t *testing.T) {
	if got := spread([]int{10, 14, 9, 11}); got != 5 {
		t.Errorf("got %d, want 5", got)
	}
	if got := spread([]int{7}); got != 0 {
		t.Errorf("got %d, want 0", got)
	}
}

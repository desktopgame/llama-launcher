package workspace

import (
	"strings"
	"testing"
)

func residentWS(entries ...Entry) *Workspace {
	return &Workspace{Name: "test", Entries: entries}
}

func TestCheckCostDisabledWhenNoMax(t *testing.T) {
	env := newTestEnv(t)
	env.addProfile(t, "big", true, intPtr(999))

	report, warnings, err := CheckCost(
		residentWS(Entry{ProfileName: "big", Resident: true}), env.profMgr, 0, true)
	if err != nil {
		t.Fatal(err)
	}
	if len(warnings) != 0 {
		t.Errorf("unexpected warnings: %v", warnings)
	}
	if report.Max != 0 || report.ResidentTotal != 0 {
		t.Errorf("expected an empty report, got %+v", report)
	}
}

// sum(resident) の超過は定常状態なのでエラー
func TestCheckCostResidentOverflowIsError(t *testing.T) {
	env := newTestEnv(t)
	env.addProfile(t, "a", true, intPtr(60))
	env.addProfile(t, "b", true, intPtr(50))

	_, _, err := CheckCost(residentWS(
		Entry{ProfileName: "a", Resident: true},
		Entry{ProfileName: "b", Resident: true},
	), env.profMgr, 100, true)
	if err == nil {
		t.Fatal("expected an error when resident cost exceeds cost_max")
	}
}

// max(非resident) を足しての超過は仮定のピークなので警告
func TestCheckCostPeakOverflowIsWarning(t *testing.T) {
	env := newTestEnv(t)
	env.addProfile(t, "a", true, intPtr(60))
	env.addProfile(t, "small", true, intPtr(10))
	env.addProfile(t, "big", true, intPtr(50))

	report, warnings, err := CheckCost(residentWS(
		Entry{ProfileName: "a", Resident: true},
		Entry{ProfileName: "small"},
		Entry{ProfileName: "big"},
	), env.profMgr, 100, true)
	if err != nil {
		t.Fatalf("peak overflow must not be an error: %v", err)
	}
	if len(warnings) != 1 || !strings.Contains(warnings[0], "peak cost 110") {
		t.Fatalf("expected a peak warning, got %v", warnings)
	}
	// on-demand グループは swap:true なので同時に載るのは1つだけ
	if report.PeakOnDemand != 50 || report.PeakProfile != "big" {
		t.Errorf("peak: got %d from %q, want 50 from \"big\"", report.PeakOnDemand, report.PeakProfile)
	}
	if report.Peak() != 110 {
		t.Errorf("Peak() = %d, want 110", report.Peak())
	}
}

func TestCheckCostFitsExactly(t *testing.T) {
	env := newTestEnv(t)
	env.addProfile(t, "a", true, intPtr(60))
	env.addProfile(t, "b", true, intPtr(40))

	_, warnings, err := CheckCost(residentWS(
		Entry{ProfileName: "a", Resident: true},
		Entry{ProfileName: "b"},
	), env.profMgr, 100, true)
	if err != nil || len(warnings) != 0 {
		t.Fatalf("expected a clean pass, got err=%v warnings=%v", err, warnings)
	}
}

func TestCheckCostMissingCost(t *testing.T) {
	env := newTestEnv(t)
	env.addProfile(t, "res", true, nil)
	env.addProfile(t, "od", true, nil)

	ws := residentWS(Entry{ProfileName: "res", Resident: true}, Entry{ProfileName: "od"})

	// --resident に名前を書いたものは cost 必須
	if _, _, err := CheckCost(ws, env.profMgr, 100, true); err == nil {
		t.Fatal("expected an error for a resident profile with no cost")
	}

	// ワークスペース経由なら警告に留める
	_, warnings, err := CheckCost(ws, env.profMgr, 100, false)
	if err != nil {
		t.Fatalf("non-strict mode must not error: %v", err)
	}
	if len(warnings) != 2 {
		t.Errorf("got %d warnings, want 2: %v", len(warnings), warnings)
	}
}

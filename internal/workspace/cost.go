package workspace

import (
	"fmt"

	"github.com/desktopgame/llama-launcher/internal/profile"
)

// CostReport summarizes the memory budget check for a workspace.
type CostReport struct {
	Max           int    // cost_max from config; 0 disables the check
	ResidentTotal int    // sum of the resident profiles' cost
	PeakOnDemand  int    // largest single on-demand profile's cost
	PeakProfile   string // which profile PeakOnDemand came from
}

// Peak returns the worst-case simultaneous cost.
func (r CostReport) Peak() int {
	return r.ResidentTotal + r.PeakOnDemand
}

// CheckCost verifies that a workspace fits within costMax.
//
// 生成される config.yaml では resident グループが swap:false（全部同時に載る）、
// on-demand グループが swap:true（同時に1つだけ）なので、ピークは
// sum(resident) + max(非resident) になる。
//
// sum(resident) の超過は常時発生する定常状態なのでエラー、ピークの超過は
// そのモデルを実際に呼んだときにしか起きない仮定なので警告に留める。
//
// strictResident makes a missing cost on a resident profile an error rather
// than a warning; use it when the caller named those profiles explicitly.
func CheckCost(
	ws *Workspace,
	profMgr *profile.Manager,
	costMax int,
	strictResident bool,
) (CostReport, []string, error) {
	report := CostReport{Max: costMax}
	if costMax <= 0 {
		return report, nil, nil // cost_max 未設定 = チェック無効
	}

	var warnings []string
	for _, entry := range ws.Entries {
		prof, err := profMgr.Load(entry.ProfileName)
		if err != nil {
			// 到達可能な構成なら生成側で既に弾かれている
			return report, warnings, fmt.Errorf("failed to load profile %q: %w", entry.ProfileName, err)
		}

		cost := 0
		if prof.Cost != nil {
			cost = *prof.Cost
		} else if entry.Resident && strictResident {
			return report, warnings, fmt.Errorf(
				"resident profile %q has no cost set (required when cost_max is configured)", entry.ProfileName)
		} else {
			warnings = append(warnings, fmt.Sprintf(
				"profile %q has no cost set; counting it as 0", entry.ProfileName))
		}

		if entry.Resident {
			report.ResidentTotal += cost
		} else if cost > report.PeakOnDemand {
			report.PeakOnDemand = cost
			report.PeakProfile = entry.ProfileName
		}
	}

	if report.ResidentTotal > costMax {
		return report, warnings, fmt.Errorf(
			"resident profiles cost %d, which exceeds cost_max %d", report.ResidentTotal, costMax)
	}
	if report.Peak() > costMax {
		peakName := report.PeakProfile
		if peakName == "" {
			peakName = "(none)"
		}
		warnings = append(warnings, fmt.Sprintf(
			"peak cost %d exceeds cost_max %d (resident %d + %q %d); loading %q may fail",
			report.Peak(), costMax, report.ResidentTotal, peakName, report.PeakOnDemand, peakName))
	}
	return report, warnings, nil
}

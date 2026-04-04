package runtime

import "testing"

func TestParseAssetName(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		want    *AssetInfo
	}{
		{
			name:  "win vulkan x64",
			input: "llama-b8660-bin-win-vulkan-x64.zip",
			want:  &AssetInfo{Tag: "b8660", OS: "win", Backend: "vulkan", Arch: "x64"},
		},
		{
			name:  "win cuda with version",
			input: "llama-b8660-bin-win-cuda-12.4-x64.zip",
			want:  &AssetInfo{Tag: "b8660", OS: "win", Backend: "cuda-12.4", Arch: "x64"},
		},
		{
			name:  "win cuda 13.1",
			input: "llama-b8660-bin-win-cuda-13.1-x64.zip",
			want:  &AssetInfo{Tag: "b8660", OS: "win", Backend: "cuda-13.1", Arch: "x64"},
		},
		{
			name:  "win cpu x64",
			input: "llama-b8660-bin-win-cpu-x64.zip",
			want:  &AssetInfo{Tag: "b8660", OS: "win", Backend: "cpu", Arch: "x64"},
		},
		{
			name:  "win cpu arm64",
			input: "llama-b8660-bin-win-cpu-arm64.zip",
			want:  &AssetInfo{Tag: "b8660", OS: "win", Backend: "cpu", Arch: "arm64"},
		},
		{
			name:  "win hip-radeon",
			input: "llama-b8660-bin-win-hip-radeon-x64.zip",
			want:  &AssetInfo{Tag: "b8660", OS: "win", Backend: "hip-radeon", Arch: "x64"},
		},
		{
			name:  "win sycl",
			input: "llama-b8660-bin-win-sycl-x64.zip",
			want:  &AssetInfo{Tag: "b8660", OS: "win", Backend: "sycl", Arch: "x64"},
		},
		{
			name:  "win opencl-adreno arm64",
			input: "llama-b8660-bin-win-opencl-adreno-arm64.zip",
			want:  &AssetInfo{Tag: "b8660", OS: "win", Backend: "opencl-adreno", Arch: "arm64"},
		},
		{
			name:  "ubuntu vulkan x64 tar.gz",
			input: "llama-b8660-bin-ubuntu-vulkan-x64.tar.gz",
			want:  &AssetInfo{Tag: "b8660", OS: "ubuntu", Backend: "vulkan", Arch: "x64"},
		},
		{
			name:  "ubuntu rocm with version",
			input: "llama-b8660-bin-ubuntu-rocm-7.2-x64.tar.gz",
			want:  &AssetInfo{Tag: "b8660", OS: "ubuntu", Backend: "rocm-7.2", Arch: "x64"},
		},
		{
			name:  "ubuntu plain x64 (cpu)",
			input: "llama-b8660-bin-ubuntu-x64.tar.gz",
			want:  &AssetInfo{Tag: "b8660", OS: "ubuntu", Backend: "cpu", Arch: "x64"},
		},
		{
			name:  "macos arm64 (cpu)",
			input: "llama-b8660-bin-macos-arm64.tar.gz",
			want:  &AssetInfo{Tag: "b8660", OS: "macos", Backend: "cpu", Arch: "arm64"},
		},
		{
			name:  "ubuntu openvino",
			input: "llama-b8660-bin-ubuntu-openvino-2026.0-x64.tar.gz",
			want:  &AssetInfo{Tag: "b8660", OS: "ubuntu", Backend: "openvino-2026.0", Arch: "x64"},
		},
		{
			name:  "cudart separate package - no bin",
			input: "cudart-llama-bin-win-cuda-12.4-x64.zip",
			want:  nil, // doesn't start with "llama-"
		},
		{
			name:  "xcframework - no bin segment match",
			input: "llama-b8660-xcframework.zip",
			want:  nil, // no "-bin-"
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			asset := Asset{Name: tt.input}
			got := ParseAssetName(asset)

			if tt.want == nil {
				if got != nil {
					t.Errorf("expected nil, got %+v", got)
				}
				return
			}

			if got == nil {
				t.Fatalf("expected %+v, got nil", tt.want)
			}
			if got.Tag != tt.want.Tag {
				t.Errorf("Tag: got %q, want %q", got.Tag, tt.want.Tag)
			}
			if got.OS != tt.want.OS {
				t.Errorf("OS: got %q, want %q", got.OS, tt.want.OS)
			}
			if got.Backend != tt.want.Backend {
				t.Errorf("Backend: got %q, want %q", got.Backend, tt.want.Backend)
			}
			if got.Arch != tt.want.Arch {
				t.Errorf("Arch: got %q, want %q", got.Arch, tt.want.Arch)
			}
		})
	}
}

func TestClassifyAssets(t *testing.T) {
	assets := []Asset{
		{Name: "llama-b8660-bin-win-vulkan-x64.zip", Size: 100},
		{Name: "llama-b8660-bin-win-cuda-12.4-x64.zip", Size: 200},
		{Name: "llama-b8660-bin-win-cpu-x64.zip", Size: 50},
		{Name: "llama-b8660-bin-ubuntu-vulkan-x64.tar.gz", Size: 150},
		{Name: "cudart-llama-bin-win-cuda-12.4-x64.zip", Size: 30},
	}

	result := ClassifyAssets(assets, "win", "x64")

	if len(result) != 3 {
		t.Fatalf("expected 3 backends, got %d: %v", len(result), result)
	}

	if info, ok := result["vulkan"]; !ok {
		t.Error("missing vulkan")
	} else if info.Backend != "vulkan" {
		t.Errorf("vulkan backend: got %q", info.Backend)
	}

	if info, ok := result["cuda-12.4"]; !ok {
		t.Error("missing cuda-12.4")
	} else if info.Asset.Size != 200 {
		t.Errorf("cuda-12.4 size: got %d", info.Asset.Size)
	}

	if _, ok := result["cpu"]; !ok {
		t.Error("missing cpu")
	}

	// ubuntu asset should NOT appear
	if _, ok := result["ubuntu"]; ok {
		t.Error("ubuntu asset should be filtered out")
	}
}

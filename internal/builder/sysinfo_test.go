package builder

import (
	"runtime"
	"testing"
)

func TestGetSystemInfo(t *testing.T) {
	info := GetSystemInfo()

	if info == nil {
		t.Fatal("GetSystemInfo returned nil")
	}

	// CPUCount should match runtime.NumCPU()
	if info.CPUCount != runtime.NumCPU() {
		t.Errorf("CPUCount = %d, want %d", info.CPUCount, runtime.NumCPU())
	}

	// CPU usage should be between 0 and 100
	if info.CPUUsage < 0 || info.CPUUsage > 100 {
		t.Errorf("CPUUsage = %f, want between 0 and 100", info.CPUUsage)
	}

	// Memory usage should be between 0 and 100
	if info.MemoryUsage < 0 || info.MemoryUsage > 100 {
		t.Errorf("MemoryUsage = %f, want between 0 and 100", info.MemoryUsage)
	}

	// Disk usage should be between 0 and 100
	if info.DiskUsage < 0 || info.DiskUsage > 100 {
		t.Errorf("DiskUsage = %f, want between 0 and 100", info.DiskUsage)
	}

	// Memory total should be > 0 on Linux
	if runtime.GOOS == "linux" && info.MemoryTotal == 0 {
		t.Error("MemoryTotal should be > 0 on Linux")
	}

	// Disk total should be > 0
	if info.DiskTotal == 0 {
		t.Error("DiskTotal should be > 0")
	}
}

func TestGetMemoryInfo(t *testing.T) {
	total, used, percentage := getMemoryInfo()

	if runtime.GOOS != "linux" {
		// On non-Linux systems, values might be 0
		t.Skip("Skipping memory test on non-Linux system")
	}

	if total == 0 {
		t.Error("Memory total should be > 0")
	}

	if used > total {
		t.Errorf("Memory used (%d) should not exceed total (%d)", used, total)
	}

	if percentage < 0 || percentage > 100 {
		t.Errorf("Memory percentage = %f, want between 0 and 100", percentage)
	}
}

func TestGetDiskInfo(t *testing.T) {
	total, used, percentage := getDiskInfo("/")

	if total == 0 {
		t.Error("Disk total should be > 0")
	}

	if used > total {
		t.Errorf("Disk used (%d) should not exceed total (%d)", used, total)
	}

	if percentage < 0 || percentage > 100 {
		t.Errorf("Disk percentage = %f, want between 0 and 100", percentage)
	}
}

func TestGetCPUUsage(t *testing.T) {
	cpuCount := runtime.NumCPU()
	usage := getCPUUsage(cpuCount)

	if usage < 0 || usage > 100 {
		t.Errorf("CPU usage = %f, want between 0 and 100", usage)
	}
}

func TestGetDiskInfoInvalidPath(t *testing.T) {
	total, used, percentage := getDiskInfo("/nonexistent/path/that/does/not/exist")

	if total != 0 || used != 0 || percentage != 0 {
		t.Errorf("Expected zero values for invalid path, got total=%d, used=%d, percentage=%f",
			total, used, percentage)
	}
}

func TestDetectArchitecture(t *testing.T) {
	arch := detectArchitecture()

	// Should return a non-empty string
	if arch == "" {
		t.Error("detectArchitecture returned empty string")
	}

	// Should be one of the known architectures
	validArchs := map[string]bool{
		"amd64": true,
		"arm64": true,
		"x86":   true,
		"arm":   true,
	}

	// The arch should be either a known one or the raw GOARCH
	if !validArchs[arch] && arch != runtime.GOARCH {
		t.Errorf("Unexpected architecture: %s", arch)
	}
}

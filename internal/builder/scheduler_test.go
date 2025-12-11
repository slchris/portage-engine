package builder

import (
	"container/heap"
	"fmt"
	"testing"
	"time"
)

// TestNewScheduler tests creating a new scheduler.
func TestNewScheduler(t *testing.T) {
	s := NewScheduler(10)
	if s == nil {
		t.Fatal("NewScheduler returned nil")
	}

	if s.maxParallel != 10 {
		t.Errorf("Expected maxParallel=10, got %d", s.maxParallel)
	}

	if s.tasks == nil {
		t.Error("Tasks map is nil")
	}

	if s.builders == nil {
		t.Error("Builders map is nil")
	}
}

// TestRegisterBuilder tests registering builders.
func TestRegisterBuilder(t *testing.T) {
	s := NewScheduler(10)

	s.RegisterBuilder("builder1", "http://builder1:9090", 2)
	s.RegisterBuilder("builder2", "http://builder2:9090", 3)

	s.mu.RLock()
	defer s.mu.RUnlock()

	if len(s.builders) != 2 {
		t.Errorf("Expected 2 builders, got %d", len(s.builders))
	}

	b1 := s.builders["builder1"]
	if b1 == nil {
		t.Fatal("Builder1 not registered")
	}

	if b1.Capacity != 2 {
		t.Errorf("Expected builder1 capacity=2, got %d", b1.Capacity)
	}

	if !b1.Enabled || !b1.Healthy {
		t.Error("Builder should be enabled and healthy by default")
	}
}

// TestUnregisterBuilder tests unregistering a builder.
func TestUnregisterBuilder(t *testing.T) {
	s := NewScheduler(10)

	s.RegisterBuilder("builder1", "http://builder1:9090", 2)
	s.RegisterBuilder("builder2", "http://builder2:9090", 3)

	s.UnregisterBuilder("builder1")

	s.mu.RLock()
	defer s.mu.RUnlock()

	if len(s.builders) != 1 {
		t.Errorf("Expected 1 builder after unregister, got %d", len(s.builders))
	}

	if _, ok := s.builders["builder1"]; ok {
		t.Error("Builder1 should be unregistered")
	}
}

// TestSetBuilderHealth tests setting builder health status.
func TestSetBuilderHealth(t *testing.T) {
	s := NewScheduler(10)
	s.RegisterBuilder("builder1", "http://builder1:9090", 2)

	s.SetBuilderHealth("builder1", false)

	s.mu.RLock()
	defer s.mu.RUnlock()

	if s.builders["builder1"].Healthy {
		t.Error("Builder should be unhealthy")
	}
}

// TestSubmitTask tests submitting tasks.
func TestSubmitTask(t *testing.T) {
	s := NewScheduler(10)

	err := s.SubmitTask("job1", "dev-lang/python", "3.11", 10, 0, []string{})
	if err != nil {
		t.Fatalf("SubmitTask failed: %v", err)
	}

	s.mu.RLock()
	defer s.mu.RUnlock()

	if len(s.tasks) != 1 {
		t.Errorf("Expected 1 task, got %d", len(s.tasks))
	}

	task := s.tasks["job1"]
	if task == nil {
		t.Fatal("Task not found")
	}

	if task.Package != "dev-lang/python" {
		t.Errorf("Expected package=dev-lang/python, got %s", task.Package)
	}

	if task.State != "ready" {
		t.Errorf("Expected state=ready (no deps), got %s", task.State)
	}

	if s.taskQueue.Len() != 1 {
		t.Errorf("Expected 1 task in queue, got %d", s.taskQueue.Len())
	}
}

// TestSubmitTaskWithDependencies tests submitting tasks with dependencies.
func TestSubmitTaskWithDependencies(t *testing.T) {
	s := NewScheduler(10)

	// Submit task with unsatisfied dependencies
	err := s.SubmitTask("job1", "dev-lang/python", "3.11", 10, 0,
		[]string{"sys-libs/glibc-2.37", "dev-libs/openssl-3.0"})
	if err != nil {
		t.Fatalf("SubmitTask failed: %v", err)
	}

	s.mu.RLock()
	task := s.tasks["job1"]
	s.mu.RUnlock()

	if task.State != "waiting_deps" {
		t.Errorf("Expected state=waiting_deps, got %s", task.State)
	}

	if s.taskQueue.Len() != 0 {
		t.Errorf("Task with unsatisfied deps should not be in queue, got %d", s.taskQueue.Len())
	}

	// Mark dependencies as completed
	s.mu.Lock()
	s.completedPkgs["sys-libs/glibc-2.37"] = true
	s.completedPkgs["dev-libs/openssl-3.0"] = true
	s.checkWaitingTasks()
	s.mu.Unlock()

	s.mu.RLock()
	task = s.tasks["job1"]
	s.mu.RUnlock()

	if task.State != "ready" {
		t.Errorf("Expected state=ready after deps satisfied, got %s", task.State)
	}

	if s.taskQueue.Len() != 1 {
		t.Errorf("Expected 1 task in queue after deps satisfied, got %d", s.taskQueue.Len())
	}
}

// TestTaskPriorityQueue tests priority queue ordering.
func TestTaskPriorityQueue(t *testing.T) {
	s := NewScheduler(10)

	// Submit tasks with different nice values and priorities
	_ = s.SubmitTask("job1", "pkg1", "1.0", 5, 10, []string{})  // nice=10
	_ = s.SubmitTask("job2", "pkg2", "1.0", 5, 0, []string{})   // nice=0 (higher priority)
	_ = s.SubmitTask("job3", "pkg3", "1.0", 5, -10, []string{}) // nice=-10 (highest priority)
	_ = s.SubmitTask("job4", "pkg4", "1.0", 10, 0, []string{})  // priority=10, nice=0

	s.mu.RLock()
	queueLen := s.taskQueue.Len()
	s.mu.RUnlock()

	if queueLen != 4 {
		t.Errorf("Expected 4 tasks in queue, got %d", queueLen)
	}

	// Pop tasks and verify ordering (use heap.Pop for correct priority order)
	s.mu.Lock()
	task1 := heap.Pop(&s.taskQueue).(*BuildTask)
	task2 := heap.Pop(&s.taskQueue).(*BuildTask)
	task3 := heap.Pop(&s.taskQueue).(*BuildTask)
	task4 := heap.Pop(&s.taskQueue).(*BuildTask)
	s.mu.Unlock()

	// Should be ordered by nice value first: -10, 0, 0, 10
	if task1.JobID != "job3" {
		t.Errorf("Expected job3 first (nice=-10), got %s", task1.JobID)
	}

	// Between job2 and job4 (both nice=0), job4 should come first (priority=10 > 5)
	if task2.JobID != "job4" {
		t.Errorf("Expected job4 second (priority=10), got %s", task2.JobID)
	}

	if task3.JobID != "job2" {
		t.Errorf("Expected job2 third (priority=5), got %s", task3.JobID)
	}

	if task4.JobID != "job1" {
		t.Errorf("Expected job1 last (nice=10), got %s", task4.JobID)
	}
}

// TestGetNextTask tests getting next task for a builder.
func TestGetNextTask(t *testing.T) {
	s := NewScheduler(10)
	s.RegisterBuilder("builder1", "http://builder1:9090", 2)

	_ = s.SubmitTask("job1", "pkg1", "1.0", 5, 0, []string{})

	task, err := s.GetNextTask("builder1")
	if err != nil {
		t.Fatalf("GetNextTask failed: %v", err)
	}

	if task.JobID != "job1" {
		t.Errorf("Expected job1, got %s", task.JobID)
	}

	if task.State != "building" {
		t.Errorf("Expected state=building, got %s", task.State)
	}

	if task.BuilderID != "builder1" {
		t.Errorf("Expected builderID=builder1, got %s", task.BuilderID)
	}

	s.mu.RLock()
	builder := s.builders["builder1"]
	s.mu.RUnlock()

	if builder.CurrentLoad != 1 {
		t.Errorf("Expected builder load=1, got %d", builder.CurrentLoad)
	}

	if s.currentParallel != 1 {
		t.Errorf("Expected currentParallel=1, got %d", s.currentParallel)
	}
}

// TestGetNextTaskBuilderCapacity tests builder capacity limits.
func TestGetNextTaskBuilderCapacity(t *testing.T) {
	s := NewScheduler(10)
	s.RegisterBuilder("builder1", "http://builder1:9090", 1)

	_ = s.SubmitTask("job1", "pkg1", "1.0", 5, 0, []string{})
	_ = s.SubmitTask("job2", "pkg2", "1.0", 5, 0, []string{})

	// Get first task
	_, err := s.GetNextTask("builder1")
	if err != nil {
		t.Fatalf("GetNextTask failed: %v", err)
	}

	// Try to get second task (should fail due to capacity)
	_, err = s.GetNextTask("builder1")
	if err == nil {
		t.Error("Expected error when builder at capacity")
	}
}

// TestGetNextTaskMaxParallel tests max parallel limit.
func TestGetNextTaskMaxParallel(t *testing.T) {
	s := NewScheduler(1) // Max 1 parallel build
	s.RegisterBuilder("builder1", "http://builder1:9090", 2)

	_ = s.SubmitTask("job1", "pkg1", "1.0", 5, 0, []string{})
	_ = s.SubmitTask("job2", "pkg2", "1.0", 5, 0, []string{})

	// Get first task
	_, err := s.GetNextTask("builder1")
	if err != nil {
		t.Fatalf("GetNextTask failed: %v", err)
	}

	// Try to get second task (should fail due to max parallel)
	_, err = s.GetNextTask("builder1")
	if err == nil {
		t.Error("Expected error when max parallel reached")
	}
}

// TestCompleteTask tests completing a task.
func TestCompleteTask(t *testing.T) {
	s := NewScheduler(10)
	s.RegisterBuilder("builder1", "http://builder1:9090", 2)

	_ = s.SubmitTask("job1", "pkg1", "1.0", 5, 0, []string{})

	task, _ := s.GetNextTask("builder1")

	err := s.CompleteTask("job1", true, nil)
	if err != nil {
		t.Fatalf("CompleteTask failed: %v", err)
	}

	s.mu.RLock()
	defer s.mu.RUnlock()

	if task.State != "completed" {
		t.Errorf("Expected state=completed, got %s", task.State)
	}

	pkgAtom := fmt.Sprintf("%s-%s", task.Package, task.Version)
	if !s.completedPkgs[pkgAtom] {
		t.Error("Package should be marked as completed")
	}

	builder := s.builders["builder1"]
	if builder.CurrentLoad != 0 {
		t.Errorf("Expected builder load=0 after completion, got %d", builder.CurrentLoad)
	}

	if s.currentParallel != 0 {
		t.Errorf("Expected currentParallel=0 after completion, got %d", s.currentParallel)
	}
}

// TestCompleteTaskFailed tests completing a failed task.
func TestCompleteTaskFailed(t *testing.T) {
	s := NewScheduler(10)
	s.RegisterBuilder("builder1", "http://builder1:9090", 2)

	_ = s.SubmitTask("job1", "pkg1", "1.0", 5, 0, []string{})

	task, _ := s.GetNextTask("builder1")

	testErr := fmt.Errorf("build failed")
	err := s.CompleteTask("job1", false, testErr)
	if err != nil {
		t.Fatalf("CompleteTask failed: %v", err)
	}

	s.mu.RLock()
	defer s.mu.RUnlock()

	if task.State != "failed" {
		t.Errorf("Expected state=failed, got %s", task.State)
	}

	if task.Error == nil {
		t.Error("Task error should be set")
	}

	pkgAtom := fmt.Sprintf("%s-%s", task.Package, task.Version)
	if s.completedPkgs[pkgAtom] {
		t.Error("Failed package should not be marked as completed")
	}
}

// TestDependencyResolution tests dependency resolution flow.
func TestDependencyResolution(t *testing.T) {
	s := NewScheduler(10)
	s.RegisterBuilder("builder1", "http://builder1:9090", 5)

	// Add dependency graph
	s.AddDependency("app-editors/vim", []string{"sys-libs/ncurses"})

	// Submit base dependency first
	_ = s.SubmitTask("job1", "sys-libs/ncurses", "6.0", 5, 0, []string{})

	// Submit dependent package
	_ = s.SubmitTask("job2", "app-editors/vim", "9.0", 5, 0, []string{"sys-libs/ncurses-6.0"})

	// Build base dependency
	task1, _ := s.GetNextTask("builder1")
	if task1.JobID != "job1" {
		t.Errorf("Expected job1 to be built first, got %s", task1.JobID)
	}

	// job2 should still be waiting
	s.mu.RLock()
	task2 := s.tasks["job2"]
	state := task2.State
	s.mu.RUnlock()

	if state != "waiting_deps" {
		t.Errorf("Expected job2 to be waiting_deps, got %s", state)
	}

	// Complete job1
	_ = s.CompleteTask("job1", true, nil)

	// Now job2 should be ready
	s.mu.RLock()
	task2 = s.tasks["job2"]
	state = task2.State
	s.mu.RUnlock()

	if state != "ready" {
		t.Errorf("Expected job2 to be ready after dep completed, got %s", state)
	}
}

// TestSchedulerGetBuilderStatus tests getting builder status from scheduler.
func TestSchedulerGetBuilderStatus(t *testing.T) {
	s := NewScheduler(10)
	s.RegisterBuilder("builder1", "http://builder1:9090", 2)
	s.RegisterBuilder("builder2", "http://builder2:9090", 3)

	status := s.GetBuilderStatus()

	if len(status) != 2 {
		t.Errorf("Expected 2 builders in status, got %d", len(status))
	}

	if status["builder1"].Capacity != 2 {
		t.Errorf("Expected builder1 capacity=2, got %d", status["builder1"].Capacity)
	}
}

// TestGetQueueStats tests getting queue statistics.
func TestGetQueueStats(t *testing.T) {
	s := NewScheduler(10)
	s.RegisterBuilder("builder1", "http://builder1:9090", 5)

	_ = s.SubmitTask("job1", "pkg1", "1.0", 5, 0, []string{})
	_ = s.SubmitTask("job2", "pkg2", "1.0", 5, 0, []string{"pkg0-1.0"})
	_ = s.SubmitTask("job3", "pkg3", "1.0", 5, 0, []string{})

	// Get one task, leaving one ready and one waiting_deps
	_, _ = s.GetNextTask("builder1")

	stats := s.GetQueueStats()

	if stats["ready"] != 1 {
		t.Errorf("Expected 1 ready task (job3), got %d", stats["ready"])
	}

	if stats["waiting_deps"] != 1 {
		t.Errorf("Expected 1 waiting_deps task (job2), got %d", stats["waiting_deps"])
	}

	if stats["building"] != 1 {
		t.Errorf("Expected 1 building task (job1), got %d", stats["building"])
	}
}

// TestParsePackageAtom tests package atom parsing.
func TestParsePackageAtom(t *testing.T) {
	tests := []struct {
		atom    string
		wantPkg string
		wantVer string
	}{
		{"dev-lang/python-3.11.8", "dev-lang/python", "3.11.8"},
		{"sys-libs/glibc-2.37", "sys-libs/glibc", "2.37"},
		{">=dev-lang/python-3.11", "dev-lang/python", "3.11"},
		{"<sys-apps/portage-3.0.0", "sys-apps/portage", "3.0.0"},
		{"app-editors/vim", "app-editors/vim", ""},
		{"sys-kernel/gentoo-sources-6.6.0", "sys-kernel/gentoo-sources", "6.6.0"},
	}

	for _, tt := range tests {
		pkg, ver := ParsePackageAtom(tt.atom)
		if pkg != tt.wantPkg {
			t.Errorf("ParsePackageAtom(%q) pkg = %q, want %q", tt.atom, pkg, tt.wantPkg)
		}
		if ver != tt.wantVer {
			t.Errorf("ParsePackageAtom(%q) ver = %q, want %q", tt.atom, ver, tt.wantVer)
		}
	}
}

// TestNiceValueValidation tests nice value clamping.
func TestNiceValueValidation(t *testing.T) {
	s := NewScheduler(10)

	// Test nice value clamping
	_ = s.SubmitTask("job1", "pkg1", "1.0", 5, -30, []string{}) // Should clamp to -20
	_ = s.SubmitTask("job2", "pkg2", "1.0", 5, 30, []string{})  // Should clamp to 19

	s.mu.RLock()
	defer s.mu.RUnlock()

	task1 := s.tasks["job1"]
	if task1.Nice != -20 {
		t.Errorf("Expected nice=-20 (clamped), got %d", task1.Nice)
	}

	task2 := s.tasks["job2"]
	if task2.Nice != 19 {
		t.Errorf("Expected nice=19 (clamped), got %d", task2.Nice)
	}
}

// TestMultiBuilderScheduling tests scheduling across multiple builders.
func TestMultiBuilderScheduling(t *testing.T) {
	s := NewScheduler(10)
	s.RegisterBuilder("builder1", "http://builder1:9090", 1)
	s.RegisterBuilder("builder2", "http://builder2:9090", 1)

	_ = s.SubmitTask("job1", "pkg1", "1.0", 5, 0, []string{})
	_ = s.SubmitTask("job2", "pkg2", "1.0", 5, 0, []string{})

	task1, err1 := s.GetNextTask("builder1")
	task2, err2 := s.GetNextTask("builder2")

	if err1 != nil || err2 != nil {
		t.Fatalf("GetNextTask failed: %v, %v", err1, err2)
	}

	if task1.BuilderID != "builder1" {
		t.Errorf("Expected task1 on builder1, got %s", task1.BuilderID)
	}

	if task2.BuilderID != "builder2" {
		t.Errorf("Expected task2 on builder2, got %s", task2.BuilderID)
	}

	if s.currentParallel != 2 {
		t.Errorf("Expected 2 parallel builds, got %d", s.currentParallel)
	}
}

// TestUnhealthyBuilderSkipped tests that unhealthy builders are skipped.
func TestUnhealthyBuilderSkipped(t *testing.T) {
	s := NewScheduler(10)
	s.RegisterBuilder("builder1", "http://builder1:9090", 2)

	s.SetBuilderHealth("builder1", false)

	_ = s.SubmitTask("job1", "pkg1", "1.0", 5, 0, []string{})

	_, err := s.GetNextTask("builder1")
	if err == nil {
		t.Error("Expected error when getting task from unhealthy builder")
	}
}

// TestTaskTimestamps tests task timestamp tracking.
func TestTaskTimestamps(t *testing.T) {
	s := NewScheduler(10)
	s.RegisterBuilder("builder1", "http://builder1:9090", 2)

	before := time.Now()
	_ = s.SubmitTask("job1", "pkg1", "1.0", 5, 0, []string{})

	s.mu.RLock()
	task := s.tasks["job1"]
	submitTime := task.SubmitTime
	s.mu.RUnlock()

	if submitTime.Before(before) {
		t.Error("Submit time should be after test start")
	}

	time.Sleep(10 * time.Millisecond)

	task, _ = s.GetNextTask("builder1")

	if task.StartTime.Before(submitTime) {
		t.Error("Start time should be after submit time")
	}
}

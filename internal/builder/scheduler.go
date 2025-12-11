// Package builder provides scheduling and dependency management for package builds.
package builder

import (
	"container/heap"
	"fmt"
	"log"
	"strings"
	"sync"
	"time"
)

// PackageDependency represents a package dependency relationship.
type PackageDependency struct {
	Package      string   `json:"package"`
	Version      string   `json:"version"`
	Dependencies []string `json:"dependencies"` // List of package atoms this depends on
}

// BuildTask represents a build task with scheduling information.
type BuildTask struct {
	JobID        string
	Package      string
	Version      string
	Priority     int      // Higher priority = built first
	Nice         int      // Nice value (-20 to 19, lower = higher priority)
	Dependencies []string // Package atoms this task depends on
	SubmitTime   time.Time
	StartTime    time.Time
	State        string // queued, waiting_deps, ready, building, completed, failed
	BuilderID    string // ID of builder assigned to this task
	Error        error
	index        int // heap index
}

// TaskQueue implements a priority queue for build tasks.
type TaskQueue []*BuildTask

func (tq TaskQueue) Len() int { return len(tq) }

func (tq TaskQueue) Less(i, j int) bool {
	// First sort by nice value (lower nice = higher priority)
	if tq[i].Nice != tq[j].Nice {
		return tq[i].Nice < tq[j].Nice
	}
	// Then by priority
	if tq[i].Priority != tq[j].Priority {
		return tq[i].Priority > tq[j].Priority
	}
	// Finally by submit time (earlier = higher priority)
	return tq[i].SubmitTime.Before(tq[j].SubmitTime)
}

func (tq TaskQueue) Swap(i, j int) {
	tq[i], tq[j] = tq[j], tq[i]
	tq[i].index = i
	tq[j].index = j
}

// Push adds a task to the priority queue.
func (tq *TaskQueue) Push(x interface{}) {
	n := len(*tq)
	task := x.(*BuildTask)
	task.index = n
	*tq = append(*tq, task)
}

// Pop removes and returns the highest priority task from the queue.
func (tq *TaskQueue) Pop() interface{} {
	old := *tq
	n := len(old)
	task := old[n-1]
	old[n-1] = nil
	task.index = -1
	*tq = old[0 : n-1]
	return task
}

// Node represents a builder with its capabilities.
type Node struct {
	ID              string
	URL             string
	Capacity        int  // Max concurrent builds
	CurrentLoad     int  // Current number of builds
	Enabled         bool // Whether this builder is enabled
	LastHealthCheck time.Time
	Healthy         bool
}

// Scheduler manages task scheduling and builder assignment.
type Scheduler struct {
	mu              sync.RWMutex
	taskQueue       TaskQueue
	tasks           map[string]*BuildTask   // JobID -> Task
	completedPkgs   map[string]bool         // Package atoms that are completed
	builders        map[string]*Node        // BuilderID -> Node
	assignedTasks   map[string][]*BuildTask // BuilderID -> Tasks
	dependencyGraph map[string][]string     // Package -> Dependencies
	maxParallel     int                     // Max parallel builds across all builders
	currentParallel int                     // Current parallel builds
	stopChan        chan struct{}
	wg              sync.WaitGroup
}

// NewScheduler creates a new task scheduler.
func NewScheduler(maxParallel int) *Scheduler {
	s := &Scheduler{
		taskQueue:       make(TaskQueue, 0),
		tasks:           make(map[string]*BuildTask),
		completedPkgs:   make(map[string]bool),
		builders:        make(map[string]*Node),
		assignedTasks:   make(map[string][]*BuildTask),
		dependencyGraph: make(map[string][]string),
		maxParallel:     maxParallel,
		stopChan:        make(chan struct{}),
	}
	heap.Init(&s.taskQueue)
	return s
}

// RegisterBuilder registers a builder node.
func (s *Scheduler) RegisterBuilder(id, url string, capacity int) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.builders[id] = &Node{
		ID:              id,
		URL:             url,
		Capacity:        capacity,
		CurrentLoad:     0,
		Enabled:         true,
		LastHealthCheck: time.Now(),
		Healthy:         true,
	}
	s.assignedTasks[id] = make([]*BuildTask, 0)

	log.Printf("Registered builder %s with capacity %d", id, capacity)
}

// UnregisterBuilder removes a builder node.
func (s *Scheduler) UnregisterBuilder(id string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	delete(s.builders, id)
	delete(s.assignedTasks, id)

	log.Printf("Unregistered builder %s", id)
}

// SetBuilderHealth updates builder health status.
func (s *Scheduler) SetBuilderHealth(id string, healthy bool) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if builder, ok := s.builders[id]; ok {
		builder.Healthy = healthy
		builder.LastHealthCheck = time.Now()
	}
}

// AddDependency adds a dependency relationship.
func (s *Scheduler) AddDependency(pkg string, deps []string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.dependencyGraph[pkg] = deps
}

// SubmitTask submits a new build task.
func (s *Scheduler) SubmitTask(jobID, pkg, version string, priority, nice int, deps []string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Validate nice value range
	if nice < -20 {
		nice = -20
	} else if nice > 19 {
		nice = 19
	}

	task := &BuildTask{
		JobID:        jobID,
		Package:      pkg,
		Version:      version,
		Priority:     priority,
		Nice:         nice,
		Dependencies: deps,
		SubmitTime:   time.Now(),
		State:        "queued",
	}

	s.tasks[jobID] = task

	// Check if dependencies are satisfied
	if s.areDependenciesSatisfied(deps) {
		task.State = "ready"
		heap.Push(&s.taskQueue, task)
	} else {
		task.State = "waiting_deps"
	}

	log.Printf("Submitted task %s for %s-%s (nice=%d, priority=%d, deps=%v)",
		jobID, pkg, version, nice, priority, deps)

	return nil
}

// areDependenciesSatisfied checks if all dependencies are completed.
func (s *Scheduler) areDependenciesSatisfied(deps []string) bool {
	for _, dep := range deps {
		if !s.completedPkgs[dep] {
			return false
		}
	}
	return true
}

// GetNextTask returns the next task to execute from a specific builder.
func (s *Scheduler) GetNextTask(builderID string) (*BuildTask, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	builder, ok := s.builders[builderID]
	if !ok {
		return nil, fmt.Errorf("builder not found: %s", builderID)
	}

	if !builder.Enabled || !builder.Healthy {
		return nil, fmt.Errorf("builder not available: %s", builderID)
	}

	if builder.CurrentLoad >= builder.Capacity {
		return nil, fmt.Errorf("builder at capacity: %s", builderID)
	}

	if s.currentParallel >= s.maxParallel {
		return nil, fmt.Errorf("max parallel builds reached")
	}

	if s.taskQueue.Len() == 0 {
		return nil, fmt.Errorf("no tasks available")
	}

	task := heap.Pop(&s.taskQueue).(*BuildTask)
	task.State = "building"
	task.BuilderID = builderID
	task.StartTime = time.Now()

	builder.CurrentLoad++
	s.currentParallel++
	s.assignedTasks[builderID] = append(s.assignedTasks[builderID], task)

	log.Printf("Assigned task %s (%s) to builder %s (load: %d/%d, parallel: %d/%d)",
		task.JobID, task.Package, builderID, builder.CurrentLoad, builder.Capacity,
		s.currentParallel, s.maxParallel)

	return task, nil
}

// CompleteTask marks a task as completed.
func (s *Scheduler) CompleteTask(jobID string, success bool, err error) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	task, ok := s.tasks[jobID]
	if !ok {
		return fmt.Errorf("task not found: %s", jobID)
	}

	if success {
		task.State = "completed"
		pkgAtom := fmt.Sprintf("%s-%s", task.Package, task.Version)
		s.completedPkgs[pkgAtom] = true

		log.Printf("Task %s completed successfully: %s", jobID, pkgAtom)

		// Check if any waiting tasks can now proceed
		s.checkWaitingTasks()
	} else {
		task.State = "failed"
		task.Error = err
		log.Printf("Task %s failed: %v", jobID, err)
	}

	// Release builder resources
	if builder, ok := s.builders[task.BuilderID]; ok {
		builder.CurrentLoad--
		s.currentParallel--

		// Remove from assigned tasks
		tasks := s.assignedTasks[task.BuilderID]
		for i, t := range tasks {
			if t.JobID == jobID {
				s.assignedTasks[task.BuilderID] = append(tasks[:i], tasks[i+1:]...)
				break
			}
		}
	}

	return nil
}

// checkWaitingTasks checks if any waiting tasks can now be executed.
func (s *Scheduler) checkWaitingTasks() {
	for _, task := range s.tasks {
		if task.State == "waiting_deps" && s.areDependenciesSatisfied(task.Dependencies) {
			task.State = "ready"
			heap.Push(&s.taskQueue, task)
			log.Printf("Task %s (%s) is now ready", task.JobID, task.Package)
		}
	}
}

// GetTaskStatus returns the status of a task.
func (s *Scheduler) GetTaskStatus(jobID string) (*BuildTask, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	task, ok := s.tasks[jobID]
	if !ok {
		return nil, fmt.Errorf("task not found: %s", jobID)
	}

	return task, nil
}

// GetBuilderStatus returns the status of all builders.
func (s *Scheduler) GetBuilderStatus() map[string]*Node {
	s.mu.RLock()
	defer s.mu.RUnlock()

	status := make(map[string]*Node)
	for id, builder := range s.builders {
		status[id] = &Node{
			ID:              builder.ID,
			URL:             builder.URL,
			Capacity:        builder.Capacity,
			CurrentLoad:     builder.CurrentLoad,
			Enabled:         builder.Enabled,
			LastHealthCheck: builder.LastHealthCheck,
			Healthy:         builder.Healthy,
		}
	}

	return status
}

// GetQueueStats returns queue statistics.
func (s *Scheduler) GetQueueStats() map[string]int {
	s.mu.RLock()
	defer s.mu.RUnlock()

	stats := map[string]int{
		"queued":       0,
		"waiting_deps": 0,
		"ready":        0,
		"building":     0,
		"completed":    0,
		"failed":       0,
	}

	for _, task := range s.tasks {
		stats[task.State]++
	}

	return stats
}

// ResolveDependencies resolves dependencies for a package using a simple parser.
// In production, this should integrate with Portage's dependency resolution.
func (s *Scheduler) ResolveDependencies(pkg string) ([]string, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if deps, ok := s.dependencyGraph[pkg]; ok {
		return deps, nil
	}

	return []string{}, nil
}

// ParsePackageAtom parses a package atom into category/package and version.
func ParsePackageAtom(atom string) (pkg, version string) {
	// Handle atoms like ">=dev-lang/python-3.11" or "dev-lang/python-3.11.8"
	atom = strings.TrimLeft(atom, ">=<~")

	parts := strings.Split(atom, "-")
	if len(parts) < 2 {
		return atom, ""
	}

	// Find where version starts (first part that starts with a digit)
	for i := len(parts) - 1; i >= 0; i-- {
		if len(parts[i]) > 0 && parts[i][0] >= '0' && parts[i][0] <= '9' {
			pkg = strings.Join(parts[:i], "-")
			version = strings.Join(parts[i:], "-")
			return
		}
	}

	return atom, ""
}

// Stop stops the scheduler.
func (s *Scheduler) Stop() {
	close(s.stopChan)
	s.wg.Wait()
}

// Package builder provides local and remote build capabilities.
package builder

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/slchris/portage-engine/internal/gpg"
)

// LocalBuildRequest represents a local package build request.
type LocalBuildRequest struct {
	PackageName  string            `json:"package_name"`
	Version      string            `json:"version"`
	UseFlags     map[string]string `json:"use_flags"`
	Environment  map[string]string `json:"environment"`
	ConfigBundle *ConfigBundle     `json:"config_bundle,omitempty"`
	PackageSpecs []PackageSpec     `json:"package_specs,omitempty"`
}

// BuildJob represents a build job with its status.
type BuildJob struct {
	ID          string                 `json:"id"`
	Request     *LocalBuildRequest     `json:"request"`
	Status      string                 `json:"status"` // queued, building, success, failed
	StartTime   time.Time              `json:"start_time"`
	EndTime     time.Time              `json:"end_time"`
	Log         string                 `json:"log"`
	ArtifactURL string                 `json:"artifact_url"`
	Error       string                 `json:"error,omitempty"`
	Metadata    map[string]interface{} `json:"metadata,omitempty"`
}

// LocalBuilder handles build jobs locally using Docker or native builds.
type LocalBuilder struct {
	workers        int
	jobQueue       chan *BuildJob
	jobs           map[string]*BuildJob
	jobsMutex      sync.RWMutex
	signer         *gpg.Signer
	workDir        string
	artifactDir    string
	useDocker      bool
	dockerImage    string
	executor       *BuildExecutor
	dockerExecutor *DockerBuildExecutor
}

// NewLocalBuilder creates a new local builder instance.
func NewLocalBuilder(workers int, signer *gpg.Signer) *LocalBuilder {
	workDir := os.Getenv("BUILD_WORK_DIR")
	if workDir == "" {
		workDir = "/var/tmp/portage-builds"
	}

	artifactDir := os.Getenv("BUILD_ARTIFACT_DIR")
	if artifactDir == "" {
		artifactDir = "/var/tmp/portage-artifacts"
	}

	useDocker := os.Getenv("USE_DOCKER") == "true"
	dockerImage := os.Getenv("DOCKER_IMAGE")
	if dockerImage == "" {
		dockerImage = "gentoo/stage3:latest"
	}

	_ = os.MkdirAll(workDir, 0750)
	_ = os.MkdirAll(artifactDir, 0750)

	lb := &LocalBuilder{
		workers:        workers,
		jobQueue:       make(chan *BuildJob, 100),
		jobs:           make(map[string]*BuildJob),
		signer:         signer,
		workDir:        workDir,
		artifactDir:    artifactDir,
		useDocker:      useDocker,
		dockerImage:    dockerImage,
		executor:       NewBuildExecutor(workDir, artifactDir),
		dockerExecutor: NewDockerBuildExecutor(workDir, artifactDir, dockerImage),
	}

	// Start worker pool
	for i := 0; i < workers; i++ {
		go lb.worker(i)
	}

	return lb
}

// SubmitBuild submits a new build job.
func (lb *LocalBuilder) SubmitBuild(req *LocalBuildRequest) (string, error) {
	jobID := uuid.New().String()

	job := &BuildJob{
		ID:        jobID,
		Request:   req,
		Status:    "queued",
		StartTime: time.Now(),
		Metadata:  make(map[string]interface{}),
	}

	lb.jobsMutex.Lock()
	lb.jobs[jobID] = job
	lb.jobsMutex.Unlock()

	lb.jobQueue <- job

	return jobID, nil
}

// GetJobStatus returns the status of a build job.
func (lb *LocalBuilder) GetJobStatus(jobID string) (*BuildJob, error) {
	lb.jobsMutex.RLock()
	defer lb.jobsMutex.RUnlock()

	job, exists := lb.jobs[jobID]
	if !exists {
		return nil, fmt.Errorf("job not found: %s", jobID)
	}

	return job, nil
}

// ListJobs returns all build jobs.
func (lb *LocalBuilder) ListJobs() []*BuildJob {
	lb.jobsMutex.RLock()
	defer lb.jobsMutex.RUnlock()

	jobs := make([]*BuildJob, 0, len(lb.jobs))
	for _, job := range lb.jobs {
		jobs = append(jobs, job)
	}

	return jobs
}

// GetStatus returns builder status.
func (lb *LocalBuilder) GetStatus() map[string]interface{} {
	lb.jobsMutex.RLock()
	defer lb.jobsMutex.RUnlock()

	queued := 0
	building := 0
	completed := 0
	failed := 0

	for _, job := range lb.jobs {
		switch job.Status {
		case "queued":
			queued++
		case "building":
			building++
		case "success":
			completed++
		case "failed":
			failed++
		}
	}

	return map[string]interface{}{
		"workers":      lb.workers,
		"queued":       queued,
		"building":     building,
		"completed":    completed,
		"failed":       failed,
		"total":        len(lb.jobs),
		"use_docker":   lb.useDocker,
		"docker_image": lb.dockerImage,
	}
}

// worker processes build jobs from the queue.
func (lb *LocalBuilder) worker(id int) {
	log.Printf("Worker %d started", id)

	for job := range lb.jobQueue {
		log.Printf("Worker %d processing job %s", id, job.ID)

		lb.jobsMutex.Lock()
		job.Status = "building"
		lb.jobsMutex.Unlock()

		var err error
		// Check if this is a new-style config bundle build
		if job.Request.ConfigBundle != nil {
			err = lb.executeConfigBundleBuild(job)
		} else {
			// Legacy build method
			if lb.useDocker {
				err = lb.executeDockerBuild(job)
			} else {
				err = lb.executeNativeBuild(job)
			}
		}

		lb.jobsMutex.Lock()
		job.EndTime = time.Now()
		if err != nil {
			job.Status = "failed"
			job.Error = err.Error()
			log.Printf("Worker %d: Job %s failed: %v", id, job.ID, err)
		} else {
			job.Status = "success"
			log.Printf("Worker %d: Job %s completed successfully", id, job.ID)
		}
		lb.jobsMutex.Unlock()
	}
}

// executeConfigBundleBuild executes a build using configuration bundle.
func (lb *LocalBuilder) executeConfigBundleBuild(job *BuildJob) error {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Hour)
	defer cancel()

	bundle := job.Request.ConfigBundle

	// If no package specs provided, create from legacy request
	if bundle.Packages == nil || len(bundle.Packages.Packages) == 0 {
		bundle.Packages = &BuildPackageSpec{
			Packages: []PackageSpec{
				{
					Atom:    job.Request.PackageName,
					Version: job.Request.Version,
				},
			},
		}
		// Convert legacy UseFlags map to slice
		if len(job.Request.UseFlags) > 0 {
			var useFlags []string
			for flag, enabled := range job.Request.UseFlags {
				if enabled == "true" || enabled == "1" {
					useFlags = append(useFlags, flag)
				} else {
					useFlags = append(useFlags, "-"+flag)
				}
			}
			bundle.Packages.Packages[0].UseFlags = useFlags
		}
	}

	var err error
	if lb.useDocker {
		err = lb.dockerExecutor.ExecuteBuild(ctx, bundle, job)
	} else {
		err = lb.executor.ExecuteBuild(ctx, bundle, job)
	}

	return err
}

// executeDockerBuild performs the build using Docker container.
func (lb *LocalBuilder) executeDockerBuild(job *BuildJob) error {
	req := job.Request

	jobWorkDir := filepath.Join(lb.workDir, job.ID)
	if err := os.MkdirAll(jobWorkDir, 0750); err != nil {
		return fmt.Errorf("failed to create work directory: %w", err)
	}
	defer func() { _ = os.RemoveAll(jobWorkDir) }()

	pkgAtom := req.PackageName
	if req.Version != "" {
		pkgAtom = fmt.Sprintf("=%s-%s", req.PackageName, req.Version)
	}

	// Build USE flags string
	useFlags := ""
	for flag, enabled := range req.UseFlags {
		if enabled == "true" || enabled == "1" {
			useFlags += flag + " "
		} else {
			useFlags += "-" + flag + " "
		}
	}

	// Create build script
	scriptPath := filepath.Join(jobWorkDir, "build.sh")
	script := fmt.Sprintf(`#!/bin/bash
set -e
export USE="%s"
emerge --sync
emerge -B --quiet %s
cp /var/cache/binpkgs/*.tbz2 /output/ 2>/dev/null || true
`, useFlags, pkgAtom)

	//nolint:gosec // G306: Script needs exec permission
	if err := os.WriteFile(scriptPath, []byte(script), 0700); err != nil {
		return fmt.Errorf("failed to write build script: %w", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Hour)
	defer cancel()

	outputDir := filepath.Join(jobWorkDir, "output")
	_ = os.MkdirAll(outputDir, 0750)

	// Run Docker container
	cmd := exec.CommandContext(ctx, "docker", "run", "--rm",
		"-v", scriptPath+":/build.sh:ro",
		"-v", outputDir+":/output",
		lb.dockerImage,
		"/bin/bash", "/build.sh")

	output, err := cmd.CombinedOutput()
	job.Log = string(output)

	if err != nil {
		return fmt.Errorf("docker build failed: %w", err)
	}

	artifact, err := lb.findLatestPackage(outputDir, req.PackageName)
	if err != nil {
		return fmt.Errorf("artifact not found: %w", err)
	}

	artifactName := filepath.Base(artifact)
	destPath := filepath.Join(lb.artifactDir, artifactName)

	copyCmd := exec.Command("cp", artifact, destPath)
	if err := copyCmd.Run(); err != nil {
		return fmt.Errorf("failed to copy artifact: %w", err)
	}

	job.ArtifactURL = destPath

	if lb.signer != nil && lb.signer.IsEnabled() {
		if err := lb.signer.SignPackage(destPath); err != nil {
			log.Printf("Warning: failed to sign package: %v", err)
		} else {
			job.Metadata["signed"] = true
			log.Printf("Package signed: %s", destPath)
		}
	}

	return nil
}

// executeNativeBuild performs the build natively using emerge.
func (lb *LocalBuilder) executeNativeBuild(job *BuildJob) error {
	req := job.Request

	jobWorkDir := filepath.Join(lb.workDir, job.ID)
	if err := os.MkdirAll(jobWorkDir, 0750); err != nil {
		return fmt.Errorf("failed to create work directory: %w", err)
	}
	defer func() { _ = os.RemoveAll(jobWorkDir) }()

	pkgAtom := req.PackageName
	if req.Version != "" {
		pkgAtom = fmt.Sprintf("=%s-%s", req.PackageName, req.Version)
	}

	env := os.Environ()
	for k, v := range req.Environment {
		env = append(env, fmt.Sprintf("%s=%s", k, v))
	}

	if len(req.UseFlags) > 0 {
		useFlags := ""
		for flag, enabled := range req.UseFlags {
			if enabled == "true" || enabled == "1" {
				useFlags += flag + " "
			} else {
				useFlags += "-" + flag + " "
			}
		}
		env = append(env, fmt.Sprintf("USE=%s", useFlags))
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Hour)
	defer cancel()

	cmd := exec.CommandContext(ctx, "emerge", "-B", "--quiet", pkgAtom)
	cmd.Env = env
	cmd.Dir = jobWorkDir

	output, err := cmd.CombinedOutput()
	job.Log = string(output)

	if err != nil {
		return fmt.Errorf("build failed: %w", err)
	}

	pkgDir := "/var/cache/binpkgs"
	artifact, err := lb.findLatestPackage(pkgDir, req.PackageName)
	if err != nil {
		return fmt.Errorf("artifact not found: %w", err)
	}

	artifactName := filepath.Base(artifact)
	destPath := filepath.Join(lb.artifactDir, artifactName)

	copyCmd := exec.Command("cp", artifact, destPath)
	if err := copyCmd.Run(); err != nil {
		return fmt.Errorf("failed to copy artifact: %w", err)
	}

	job.ArtifactURL = destPath

	if lb.signer != nil && lb.signer.IsEnabled() {
		if err := lb.signer.SignPackage(destPath); err != nil {
			log.Printf("Warning: failed to sign package: %v", err)
		} else {
			job.Metadata["signed"] = true
			log.Printf("Package signed: %s", destPath)
		}
	}

	return nil
}

// findLatestPackage finds the most recently created package file.
func (lb *LocalBuilder) findLatestPackage(dir, _ string) (string, error) {
	var latestFile string
	var latestTime time.Time

	err := filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}

		if !info.IsDir() && filepath.Ext(path) == ".tbz2" {
			if info.ModTime().After(latestTime) {
				latestFile = path
				latestTime = info.ModTime()
			}
		}

		return nil
	})

	if err != nil {
		return "", err
	}

	if latestFile == "" {
		return "", fmt.Errorf("no package found")
	}

	return latestFile, nil
}

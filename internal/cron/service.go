package cron

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

// OnJob is the callback invoked when a job fires.
// It receives the job and returns the agent's response.
type OnJob func(ctx context.Context, job *Job) (string, error)

// Service manages scheduled jobs.
type Service struct {
	storePath string
	onJob     OnJob

	mu    sync.Mutex
	store *Store
}

// NewService creates a new cron service.
func NewService(storePath string, onJob OnJob) *Service {
	return &Service{
		storePath: storePath,
		onJob:     onJob,
	}
}

// Run starts the cron service. It blocks until ctx is cancelled.
func (s *Service) Run(ctx context.Context) {
	s.mu.Lock()
	s.loadStore()
	s.recomputeNextRuns()
	s.saveStore()
	jobCount := len(s.store.Jobs)
	s.mu.Unlock()

	slog.Info("Cron service started", "jobs", jobCount)

	ticker := time.NewTicker(15 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			slog.Info("Cron service stopped")
			return
		case <-ticker.C:
			s.onTimer(ctx)
		}
	}
}

func (s *Service) onTimer(ctx context.Context) {
	s.mu.Lock()
	if s.store == nil {
		s.mu.Unlock()
		return
	}

	now := nowMs()
	var due []*Job
	for _, j := range s.store.Jobs {
		if j.Enabled && j.State.NextRunAtMs > 0 && now >= j.State.NextRunAtMs {
			due = append(due, j)
		}
	}
	s.mu.Unlock()

	for _, job := range due {
		s.executeJob(ctx, job)
	}

	if len(due) > 0 {
		s.mu.Lock()
		s.saveStore()
		s.mu.Unlock()
	}
}

func (s *Service) executeJob(ctx context.Context, job *Job) {
	startMs := nowMs()
	slog.Info("Cron: executing job", "name", job.Name, "id", job.ID)

	if s.onJob != nil {
		_, err := s.onJob(ctx, job)
		if err != nil {
			slog.Error("Cron: job failed", "name", job.Name, "err", err)
			s.mu.Lock()
			job.State.LastStatus = "error"
			job.State.LastError = err.Error()
			s.mu.Unlock()
		} else {
			slog.Info("Cron: job completed", "name", job.Name)
			s.mu.Lock()
			job.State.LastStatus = "ok"
			job.State.LastError = ""
			s.mu.Unlock()
		}
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	job.State.LastRunAtMs = startMs
	job.UpdatedAtMs = nowMs()

	if job.Schedule.Kind == "at" {
		if job.DeleteAfterRun {
			s.removeJobLocked(job.ID)
		} else {
			job.Enabled = false
			job.State.NextRunAtMs = 0
		}
	} else {
		job.State.NextRunAtMs = computeNextRun(job.Schedule, nowMs())
	}
}

// --- Public API ---

// ListJobs returns all jobs, optionally including disabled ones.
func (s *Service) ListJobs(includeDisabled bool) []*Job {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.loadStore()

	var result []*Job
	for _, j := range s.store.Jobs {
		if includeDisabled || j.Enabled {
			result = append(result, j)
		}
	}
	sort.Slice(result, func(i, j int) bool {
		a, b := result[i].State.NextRunAtMs, result[j].State.NextRunAtMs
		if a == 0 {
			return false
		}
		if b == 0 {
			return true
		}
		return a < b
	})
	return result
}

// AddJob creates a new scheduled job.
func (s *Service) AddJob(name string, schedule Schedule, message string, deliver bool, channel, to string, deleteAfterRun bool) *Job {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.loadStore()

	now := nowMs()
	job := &Job{
		ID:      shortID(),
		Name:    name,
		Enabled: true,
		Schedule: schedule,
		Payload: Payload{
			Kind:    "agent_turn",
			Message: message,
			Deliver: deliver,
			Channel: channel,
			To:      to,
		},
		State: JobState{
			NextRunAtMs: computeNextRun(schedule, now),
		},
		CreatedAtMs:    now,
		UpdatedAtMs:    now,
		DeleteAfterRun: deleteAfterRun,
	}

	s.store.Jobs = append(s.store.Jobs, job)
	s.saveStore()
	slog.Info("Cron: added job", "name", name, "id", job.ID)
	return job
}

// RemoveJob removes a job by ID.
func (s *Service) RemoveJob(id string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.loadStore()

	removed := s.removeJobLocked(id)
	if removed {
		s.saveStore()
		slog.Info("Cron: removed job", "id", id)
	}
	return removed
}

func (s *Service) removeJobLocked(id string) bool {
	before := len(s.store.Jobs)
	filtered := make([]*Job, 0, len(s.store.Jobs))
	for _, j := range s.store.Jobs {
		if j.ID != id {
			filtered = append(filtered, j)
		}
	}
	s.store.Jobs = filtered
	return len(s.store.Jobs) < before
}

// EnableJob enables or disables a job.
func (s *Service) EnableJob(id string, enabled bool) *Job {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.loadStore()

	for _, j := range s.store.Jobs {
		if j.ID == id {
			j.Enabled = enabled
			j.UpdatedAtMs = nowMs()
			if enabled {
				j.State.NextRunAtMs = computeNextRun(j.Schedule, nowMs())
			} else {
				j.State.NextRunAtMs = 0
			}
			s.saveStore()
			return j
		}
	}
	return nil
}

// JobCount returns the number of jobs.
func (s *Service) JobCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.loadStore()
	return len(s.store.Jobs)
}

// --- Persistence ---

func (s *Service) loadStore() {
	if s.store != nil {
		return
	}

	data, err := os.ReadFile(s.storePath)
	if err != nil {
		s.store = &Store{Version: 1}
		return
	}

	var store Store
	if err := json.Unmarshal(data, &store); err != nil {
		slog.Warn("Failed to parse cron store", "err", err)
		s.store = &Store{Version: 1}
		return
	}
	s.store = &store
}

func (s *Service) saveStore() {
	if s.store == nil {
		return
	}
	dir := filepath.Dir(s.storePath)
	os.MkdirAll(dir, 0o755)

	data, err := json.MarshalIndent(s.store, "", "  ")
	if err != nil {
		slog.Error("Failed to marshal cron store", "err", err)
		return
	}
	if err := os.WriteFile(s.storePath, data, 0o644); err != nil {
		slog.Error("Failed to save cron store", "err", err)
	}
}

func (s *Service) recomputeNextRuns() {
	if s.store == nil {
		return
	}
	now := nowMs()
	for _, j := range s.store.Jobs {
		if j.Enabled {
			j.State.NextRunAtMs = computeNextRun(j.Schedule, now)
		}
	}
}

// --- Scheduling ---

// computeNextRun calculates the next run time in ms for a schedule.
func computeNextRun(sched Schedule, now int64) int64 {
	switch sched.Kind {
	case "at":
		if sched.AtMs > now {
			return sched.AtMs
		}
		return 0
	case "every":
		if sched.EveryMs <= 0 {
			return 0
		}
		return now + sched.EveryMs
	case "cron":
		return nextCronRun(sched.Expr, sched.TZ, now)
	}
	return 0
}

// nextCronRun computes the next run time for a standard 5-field cron expression.
// Supports: minute hour day-of-month month day-of-week
// Fields support: numbers, *, */N, ranges (a-b), lists (a,b,c).
func nextCronRun(expr, tz string, nowMs int64) int64 {
	if expr == "" {
		return 0
	}

	loc := time.Local
	if tz != "" {
		if l, err := time.LoadLocation(tz); err == nil {
			loc = l
		}
	}

	fields := strings.Fields(expr)
	if len(fields) != 5 {
		slog.Warn("Invalid cron expression", "expr", expr)
		return 0
	}

	minutes := parseCronField(fields[0], 0, 59)
	hours := parseCronField(fields[1], 0, 23)
	doms := parseCronField(fields[2], 1, 31)
	months := parseCronField(fields[3], 1, 12)
	dows := parseCronField(fields[4], 0, 6)

	if minutes == nil || hours == nil || doms == nil || months == nil || dows == nil {
		slog.Warn("Failed to parse cron expression", "expr", expr)
		return 0
	}

	t := time.UnixMilli(nowMs).In(loc)
	// Start from next minute
	t = t.Truncate(time.Minute).Add(time.Minute)

	// Search up to 366 days ahead
	end := t.Add(366 * 24 * time.Hour)
	for t.Before(end) {
		if months[int(t.Month())] && doms[t.Day()] && dows[int(t.Weekday())] &&
			hours[t.Hour()] && minutes[t.Minute()] {
			return t.UnixMilli()
		}

		// Advance: skip months/days that don't match first for efficiency
		if !months[int(t.Month())] {
			// Jump to first day of next month
			t = time.Date(t.Year(), t.Month()+1, 1, 0, 0, 0, 0, loc)
			continue
		}
		if !doms[t.Day()] || !dows[int(t.Weekday())] {
			// Jump to next day
			t = time.Date(t.Year(), t.Month(), t.Day()+1, 0, 0, 0, 0, loc)
			continue
		}
		if !hours[t.Hour()] {
			// Jump to next hour
			t = t.Truncate(time.Hour).Add(time.Hour)
			continue
		}
		// Advance one minute
		t = t.Add(time.Minute)
	}

	return 0
}

// parseCronField parses a single cron field into a set of matching values.
// Returns a map where map[value] = true for each matching value.
func parseCronField(field string, min, max int) map[int]bool {
	result := make(map[int]bool)

	for _, part := range strings.Split(field, ",") {
		part = strings.TrimSpace(part)

		// Handle */N
		if strings.HasPrefix(part, "*/") {
			step, err := strconv.Atoi(part[2:])
			if err != nil || step <= 0 {
				return nil
			}
			for i := min; i <= max; i += step {
				result[i] = true
			}
			continue
		}

		// Handle *
		if part == "*" {
			for i := min; i <= max; i++ {
				result[i] = true
			}
			continue
		}

		// Handle range a-b or a-b/step
		if strings.Contains(part, "-") {
			rangeParts := strings.SplitN(part, "/", 2)
			bounds := strings.SplitN(rangeParts[0], "-", 2)
			if len(bounds) != 2 {
				return nil
			}
			lo, err1 := strconv.Atoi(bounds[0])
			hi, err2 := strconv.Atoi(bounds[1])
			if err1 != nil || err2 != nil || lo < min || hi > max {
				return nil
			}
			step := 1
			if len(rangeParts) == 2 {
				s, err := strconv.Atoi(rangeParts[1])
				if err != nil || s <= 0 {
					return nil
				}
				step = s
			}
			for i := lo; i <= hi; i += step {
				result[i] = true
			}
			continue
		}

		// Single value
		val, err := strconv.Atoi(part)
		if err != nil || val < min || val > max {
			return nil
		}
		result[val] = true
	}

	return result
}

// --- Helpers ---

func shortID() string {
	b := make([]byte, 4)
	rand.Read(b)
	return hex.EncodeToString(b)
}

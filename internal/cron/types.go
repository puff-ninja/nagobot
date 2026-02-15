package cron

import "time"

// Schedule defines when a job should run.
type Schedule struct {
	Kind    string `json:"kind"`              // "at", "every", or "cron"
	AtMs    int64  `json:"atMs,omitempty"`     // for "at": unix timestamp in ms
	EveryMs int64  `json:"everyMs,omitempty"`  // for "every": interval in ms
	Expr    string `json:"expr,omitempty"`     // for "cron": cron expression
	TZ      string `json:"tz,omitempty"`       // timezone for cron expressions
}

// Payload defines what to do when a job fires.
type Payload struct {
	Kind    string `json:"kind"`              // "agent_turn"
	Message string `json:"message"`
	Deliver bool   `json:"deliver"`
	Channel string `json:"channel,omitempty"`
	To      string `json:"to,omitempty"`
}

// JobState holds runtime state of a job.
type JobState struct {
	NextRunAtMs int64  `json:"nextRunAtMs,omitempty"`
	LastRunAtMs int64  `json:"lastRunAtMs,omitempty"`
	LastStatus  string `json:"lastStatus,omitempty"` // "ok" or "error"
	LastError   string `json:"lastError,omitempty"`
}

// Job represents a scheduled job.
type Job struct {
	ID             string   `json:"id"`
	Name           string   `json:"name"`
	Enabled        bool     `json:"enabled"`
	Schedule       Schedule `json:"schedule"`
	Payload        Payload  `json:"payload"`
	State          JobState `json:"state"`
	CreatedAtMs    int64    `json:"createdAtMs"`
	UpdatedAtMs    int64    `json:"updatedAtMs"`
	DeleteAfterRun bool     `json:"deleteAfterRun"`
}

// Store holds persisted cron jobs.
type Store struct {
	Version int    `json:"version"`
	Jobs    []*Job `json:"jobs"`
}

func nowMs() int64 {
	return time.Now().UnixMilli()
}

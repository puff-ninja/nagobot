package tool

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/joebot/nagobot/internal/cron"
)

// CronTool allows the agent to schedule, list, and remove cron jobs.
type CronTool struct {
	service        *cron.Service
	defaultChannel string
	defaultChatID  string
}

// NewCronTool creates a new cron tool.
func NewCronTool(service *cron.Service) *CronTool {
	return &CronTool{service: service}
}

// SetContext sets the current channel/chat context for job delivery.
func (t *CronTool) SetContext(channel, chatID string) {
	t.defaultChannel = channel
	t.defaultChatID = chatID
}

func (t *CronTool) Name() string { return "cron" }
func (t *CronTool) Description() string {
	return "Schedule reminders and recurring tasks. Actions: add, list, remove."
}
func (t *CronTool) Parameters() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"action": map[string]any{
				"type":        "string",
				"enum":        []string{"add", "list", "remove"},
				"description": "Action to perform",
			},
			"message": map[string]any{
				"type":        "string",
				"description": "Reminder message (for add)",
			},
			"every_seconds": map[string]any{
				"type":        "integer",
				"description": "Interval in seconds (for recurring tasks)",
			},
			"cron_expr": map[string]any{
				"type":        "string",
				"description": "Cron expression like '0 9 * * *' (for scheduled tasks)",
			},
			"at": map[string]any{
				"type":        "string",
				"description": "ISO datetime for one-time execution (e.g. '2026-02-12T10:30:00')",
			},
			"job_id": map[string]any{
				"type":        "string",
				"description": "Job ID (for remove)",
			},
		},
		"required": []string{"action"},
	}
}

func (t *CronTool) Execute(_ context.Context, params map[string]any) (ToolResult, error) {
	action, err := requireStringParam(params, "action")
	if err != nil {
		return ToolResult{}, err
	}

	switch action {
	case "add":
		return t.addJob(params)
	case "list":
		return t.listJobs()
	case "remove":
		return t.removeJob(params)
	default:
		return ToolResult{Content: fmt.Sprintf("Unknown action: %s", action)}, nil
	}
}

func (t *CronTool) addJob(params map[string]any) (ToolResult, error) {
	message := getStringParam(params, "message")
	if message == "" {
		return ToolResult{Content: "Error: message is required for add"}, nil
	}
	if t.defaultChannel == "" || t.defaultChatID == "" {
		return ToolResult{Content: "Error: no session context (channel/chat_id)"}, nil
	}

	var schedule cron.Schedule
	deleteAfter := false

	everySeconds := getIntParam(params, "every_seconds")
	cronExpr := getStringParam(params, "cron_expr")
	at := getStringParam(params, "at")

	switch {
	case everySeconds > 0:
		schedule = cron.Schedule{Kind: "every", EveryMs: int64(everySeconds) * 1000}
	case cronExpr != "":
		schedule = cron.Schedule{Kind: "cron", Expr: cronExpr}
	case at != "":
		dt, err := time.Parse("2006-01-02T15:04:05", at)
		if err != nil {
			dt, err = time.Parse("2006-01-02 15:04:05", at)
		}
		if err != nil {
			return ToolResult{Content: fmt.Sprintf("Error: invalid datetime format: %s", at)}, nil
		}
		schedule = cron.Schedule{Kind: "at", AtMs: dt.UnixMilli()}
		deleteAfter = true
	default:
		return ToolResult{Content: "Error: either every_seconds, cron_expr, or at is required"}, nil
	}

	name := message
	if len(name) > 30 {
		name = name[:30]
	}

	job := t.service.AddJob(name, schedule, message, true, t.defaultChannel, t.defaultChatID, deleteAfter)
	return ToolResult{Content: fmt.Sprintf("Created job '%s' (id: %s)", job.Name, job.ID)}, nil
}

func (t *CronTool) listJobs() (ToolResult, error) {
	jobs := t.service.ListJobs(false)
	if len(jobs) == 0 {
		return ToolResult{Content: "No scheduled jobs."}, nil
	}

	var sb strings.Builder
	sb.WriteString("Scheduled jobs:\n")
	for _, j := range jobs {
		sb.WriteString(fmt.Sprintf("- %s (id: %s, %s)\n", j.Name, j.ID, j.Schedule.Kind))
	}
	return ToolResult{Content: strings.TrimRight(sb.String(), "\n")}, nil
}

func (t *CronTool) removeJob(params map[string]any) (ToolResult, error) {
	jobID := getStringParam(params, "job_id")
	if jobID == "" {
		return ToolResult{Content: "Error: job_id is required for remove"}, nil
	}
	if t.service.RemoveJob(jobID) {
		return ToolResult{Content: fmt.Sprintf("Removed job %s", jobID)}, nil
	}
	return ToolResult{Content: fmt.Sprintf("Job %s not found", jobID)}, nil
}

// getIntParam extracts an integer parameter.
func getIntParam(params map[string]any, key string) int {
	v, ok := params[key]
	if !ok {
		return 0
	}
	switch val := v.(type) {
	case float64:
		return int(val)
	case int:
		return val
	case int64:
		return int(val)
	}
	return 0
}

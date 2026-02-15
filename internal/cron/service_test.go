package cron

import (
	"testing"
	"time"
)

func TestParseCronField(t *testing.T) {
	tests := []struct {
		field string
		min   int
		max   int
		want  []int
	}{
		{"*", 0, 59, nil},
		{"5", 0, 59, []int{5}},
		{"0", 0, 23, []int{0}},
		{"*/15", 0, 59, []int{0, 15, 30, 45}},
		{"1-5", 0, 6, []int{1, 2, 3, 4, 5}},
		{"1,3,5", 1, 12, []int{1, 3, 5}},
		{"1-10/3", 0, 59, []int{1, 4, 7, 10}},
	}

	for _, tt := range tests {
		result := parseCronField(tt.field, tt.min, tt.max)
		if result == nil {
			t.Errorf("parseCronField(%q, %d, %d) returned nil", tt.field, tt.min, tt.max)
			continue
		}

		if tt.field == "*" {
			for i := tt.min; i <= tt.max; i++ {
				if !result[i] {
					t.Errorf("parseCronField(%q): missing value %d", tt.field, i)
				}
			}
			continue
		}

		if tt.want != nil {
			if len(result) != len(tt.want) {
				t.Errorf("parseCronField(%q): got %d values, want %d", tt.field, len(result), len(tt.want))
				continue
			}
			for _, v := range tt.want {
				if !result[v] {
					t.Errorf("parseCronField(%q): missing value %d", tt.field, v)
				}
			}
		}
	}
}

func TestParseCronFieldInvalid(t *testing.T) {
	tests := []struct {
		field string
		min   int
		max   int
	}{
		{"99", 0, 59},
		{"-1", 0, 59},
		{"abc", 0, 59},
		{"*/0", 0, 59},
	}

	for _, tt := range tests {
		result := parseCronField(tt.field, tt.min, tt.max)
		if result != nil {
			t.Errorf("parseCronField(%q) should return nil for invalid input, got %v", tt.field, result)
		}
	}
}

func TestNextCronRun(t *testing.T) {
	now := time.Date(2026, 2, 15, 8, 0, 0, 0, time.Local)
	next := nextCronRun("0 9 * * *", "", now.UnixMilli())
	if next == 0 {
		t.Fatal("nextCronRun returned 0")
	}
	nextTime := time.UnixMilli(next).In(time.Local)
	if nextTime.Hour() != 9 || nextTime.Minute() != 0 {
		t.Errorf("expected 09:00, got %02d:%02d", nextTime.Hour(), nextTime.Minute())
	}
	if nextTime.Before(now) {
		t.Error("next run should be after now")
	}
}

func TestNextCronRunEvery15Min(t *testing.T) {
	now := time.Date(2026, 2, 15, 10, 7, 30, 0, time.Local)
	next := nextCronRun("*/15 * * * *", "", now.UnixMilli())
	if next == 0 {
		t.Fatal("nextCronRun returned 0")
	}
	nextTime := time.UnixMilli(next).In(time.Local)
	if nextTime.Minute() != 15 {
		t.Errorf("expected minute 15, got %d", nextTime.Minute())
	}
}

func TestComputeNextRun(t *testing.T) {
	now := nowMs()

	future := now + 60000
	result := computeNextRun(Schedule{Kind: "at", AtMs: future}, now)
	if result != future {
		t.Errorf("at future: got %d, want %d", result, future)
	}

	past := now - 60000
	result = computeNextRun(Schedule{Kind: "at", AtMs: past}, now)
	if result != 0 {
		t.Errorf("at past: got %d, want 0", result)
	}

	result = computeNextRun(Schedule{Kind: "every", EveryMs: 30000}, now)
	expected := now + 30000
	if result != expected {
		t.Errorf("every: got %d, want %d", result, expected)
	}

	result = computeNextRun(Schedule{Kind: "every", EveryMs: 0}, now)
	if result != 0 {
		t.Errorf("every zero: got %d, want 0", result)
	}
}

func TestShortID(t *testing.T) {
	id1 := shortID()
	id2 := shortID()
	if len(id1) != 8 {
		t.Errorf("ShortID length: got %d, want 8", len(id1))
	}
	if id1 == id2 {
		t.Error("two ShortIDs should be different")
	}
}

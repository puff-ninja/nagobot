package config

import (
	"fmt"
	"reflect"
	"sort"
	"strings"
)

// Validate checks the configuration for invalid or missing values.
func (c *Config) Validate() error {
	if errs := c.validate(); len(errs) > 0 {
		return fmt.Errorf("config validation failed:\n  - %s", strings.Join(errs, "\n  - "))
	}
	return nil
}

func (c *Config) validate() []string {
	var errs []string

	// agents.defaults
	d := c.Agents.Defaults
	if d.MaxTokens < 0 {
		errs = append(errs, "agents.defaults.maxTokens must be non-negative")
	}
	if d.MaxToolIterations < 0 {
		errs = append(errs, "agents.defaults.maxToolIterations must be non-negative")
	}
	if d.MemoryWindow < 0 {
		errs = append(errs, "agents.defaults.memoryWindow must be non-negative")
	}
	if d.ContextLimit < 0 {
		errs = append(errs, "agents.defaults.contextLimit must be non-negative")
	}
	if d.Temperature < 0 || d.Temperature > 2 {
		errs = append(errs, "agents.defaults.temperature must be between 0 and 2")
	}

	// channels.discord
	dc := c.Channels.Discord
	if dc.Enabled && dc.Token == "" {
		errs = append(errs, "channels.discord.token is required when discord is enabled")
	}

	// tools.exec
	if c.Tools.Exec.Timeout < 0 {
		errs = append(errs, "tools.exec.timeout must be non-negative")
	}

	// services.heartbeat
	hb := c.Services.Heartbeat
	if hb.Enabled && hb.IntervalS <= 0 {
		errs = append(errs, "services.heartbeat.intervalSeconds must be positive when enabled")
	}

	return errs
}

// CheckUnknownFields walks the raw config map and returns paths of any keys
// that do not correspond to known Config struct fields.
func CheckUnknownFields(raw map[string]any) []string {
	result := checkUnknownFields(raw, reflect.TypeOf(Config{}), "")
	sort.Strings(result)
	return result
}

func checkUnknownFields(data map[string]any, t reflect.Type, prefix string) []string {
	t = derefType(t)

	switch t.Kind() {
	case reflect.Map:
		// Map keys are user-defined (e.g. MCP server names); check values only.
		elemType := derefType(t.Elem())
		if elemType.Kind() != reflect.Struct {
			return nil
		}
		var unknown []string
		for key, val := range data {
			if nested, ok := val.(map[string]any); ok {
				unknown = append(unknown, checkUnknownFields(nested, elemType, joinPath(prefix, key))...)
			}
		}
		return unknown

	case reflect.Struct:
		known := jsonFieldMap(t)
		var unknown []string
		for key, val := range data {
			ft, ok := known[key]
			if !ok {
				unknown = append(unknown, joinPath(prefix, key))
				continue
			}
			if nested, ok := val.(map[string]any); ok {
				unknown = append(unknown, checkUnknownFields(nested, ft, joinPath(prefix, key))...)
			}
		}
		return unknown

	default:
		return nil
	}
}

func jsonFieldMap(t reflect.Type) map[string]reflect.Type {
	m := make(map[string]reflect.Type, t.NumField())
	for i := 0; i < t.NumField(); i++ {
		f := t.Field(i)
		tag := f.Tag.Get("json")
		if tag == "" || tag == "-" {
			continue
		}
		name := strings.Split(tag, ",")[0]
		if name != "" {
			m[name] = f.Type
		}
	}
	return m
}

func derefType(t reflect.Type) reflect.Type {
	for t.Kind() == reflect.Ptr {
		t = t.Elem()
	}
	return t
}

func joinPath(prefix, key string) string {
	if prefix == "" {
		return key
	}
	return prefix + "." + key
}

package tools

import (
	"context"
	"strings"
	"testing"
)

// ---------------------------------------------------------------------------
// TestWeatherTool_Info
// ---------------------------------------------------------------------------

func TestWeatherTool_Info(t *testing.T) {
	tl, err := NewWeatherTool()
	if err != nil {
		t.Fatalf("NewWeatherTool: %v", err)
	}
	info, err := tl.Info(context.Background())
	if err != nil {
		t.Fatalf("Info: %v", err)
	}
	if info.Name != "get_weather" {
		t.Errorf("Name = %q, want %q", info.Name, "get_weather")
	}
	if info.Desc == "" {
		t.Error("expected non-empty description")
	}
}

// ---------------------------------------------------------------------------
// TestWeatherTool_Invoke
// ---------------------------------------------------------------------------

func TestWeatherTool_Invoke(t *testing.T) {
	tl, err := NewWeatherTool()
	if err != nil {
		t.Fatalf("NewWeatherTool: %v", err)
	}

	result, err := tl.InvokableRun(context.Background(), `{"city":"Tokyo"}`)
	if err != nil {
		t.Fatalf("InvokableRun: %v", err)
	}
	if result == "" {
		t.Error("expected non-empty result")
	}
	// Result should contain temperature marker.
	if !strings.Contains(result, "°C") {
		t.Errorf("expected result to contain °C, got: %s", result)
	}
}

// ---------------------------------------------------------------------------
// TestWeatherTool_EmptyCity
// ---------------------------------------------------------------------------

func TestWeatherTool_EmptyCity(t *testing.T) {
	tl, err := NewWeatherTool()
	if err != nil {
		t.Fatalf("NewWeatherTool: %v", err)
	}

	_, err = tl.InvokableRun(context.Background(), `{"city":""}`)
	if err == nil {
		t.Fatal("expected error for empty city, got nil")
	}
}

// ---------------------------------------------------------------------------
// TestWeatherTool_Deterministic
// ---------------------------------------------------------------------------

func TestWeatherTool_Deterministic(t *testing.T) {
	tl, err := NewWeatherTool()
	if err != nil {
		t.Fatalf("NewWeatherTool: %v", err)
	}

	r1, err := tl.InvokableRun(context.Background(), `{"city":"Paris"}`)
	if err != nil {
		t.Fatalf("first call: %v", err)
	}
	r2, err := tl.InvokableRun(context.Background(), `{"city":"Paris"}`)
	if err != nil {
		t.Fatalf("second call: %v", err)
	}
	if r1 != r2 {
		t.Errorf("non-deterministic: first=%q second=%q", r1, r2)
	}
}

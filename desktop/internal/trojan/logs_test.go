package trojan

import (
	"testing"
	"time"

	"github.com/litai686/trojan-go/desktop/internal/store"
)

func TestLogHubRingBufferAndBatching(t *testing.T) {
	batches := make(chan []store.LogEntry, 4)
	hub := NewLogHub(func(name string, data any) {
		if name != "runtime:log" {
			return
		}
		payload, ok := data.(map[string]any)
		if !ok {
			return
		}
		lines, ok := payload["lines"].([]store.LogEntry)
		if ok {
			batches <- lines
		}
	}, 100)
	hub.Add("stdout", "[INFO]  2026/10/23 10:00:00 first")
	hub.Add("stdout", "[ERROR] 2026/10/23 10:00:01 second")
	for i := 0; i < 103; i++ {
		hub.Add("stderr", "filler")
	}
	hub.Start()
	defer hub.Stop()

	select {
	case batch := <-batches:
		if len(batch) != 105 {
			t.Fatalf("expected 105 batched lines, got %d", len(batch))
		}
		if batch[0].Level != "INFO" || batch[1].Level != "ERROR" || batch[2].Level != "INFO" {
			t.Fatalf("unexpected levels: %#v", batch[:3])
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for log batch")
	}

	snapshot := hub.Snapshot(200)
	if len(snapshot) != 100 {
		t.Fatalf("ring buffer should keep 100 lines, got %d", len(snapshot))
	}
	if snapshot[0].Seq != 6 || snapshot[len(snapshot)-1].Seq != 105 {
		t.Fatalf("unexpected sequence numbers: first=%d last=%d", snapshot[0].Seq, snapshot[len(snapshot)-1].Seq)
	}
}

func TestLogHubClear(t *testing.T) {
	cleared := make(chan struct{}, 1)
	hub := NewLogHub(func(name string, data any) {
		if name == "runtime:cleared" {
			cleared <- struct{}{}
		}
	}, 100)
	hub.AddSystem("hello")
	hub.Clear()
	select {
	case <-cleared:
	case <-time.After(time.Second):
		t.Fatal("clear event not emitted")
	}
	if len(hub.Snapshot(0)) != 0 {
		t.Fatal("log buffer not cleared")
	}
}

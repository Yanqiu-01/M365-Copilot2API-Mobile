package web

import (
	"context"
	"strings"
	"testing"
)

func TestBenchTaskCatalogAPKShape(t *testing.T) {
	tasks := benchTaskCatalog()
	if len(tasks) != 8 {
		t.Fatalf("task count=%d", len(tasks))
	}
	want := []string{"bugfix", "debug", "refactor", "algorithm", "shift", "sales", "ledger", "route"}
	for i, id := range want {
		if tasks[i].ID != id {
			t.Fatalf("task %d id=%q want %q", i, tasks[i].ID, id)
		}
		if tasks[i].Category == "" || tasks[i].Title == "" || tasks[i].Detail == "" {
			t.Fatalf("incomplete task metadata: %#v", tasks[i])
		}
	}
	for _, task := range tasks[:4] {
		if task.Category != "coding" {
			t.Fatalf("coding task category=%q", task.Category)
		}
	}
	for _, task := range tasks[4:] {
		if task.Category != "reasoning" {
			t.Fatalf("reasoning task category=%q", task.Category)
		}
	}
}

func TestBenchWeightedAverageAPKSplit(t *testing.T) {
	average, coding, reasoning := benchWeightedAverage([]benchTaskResult{
		{benchTask: benchTask{Category: "coding"}, NetScore: 1},
		{benchTask: benchTask{Category: "reasoning"}, NetScore: 0.5},
	})
	if coding != 1 || reasoning != 0.5 || average != 0.8 {
		t.Fatalf("average=%v coding=%v reasoning=%v", average, coding, reasoning)
	}
}

func TestBenchmarkStoreSnapshotLogUpdateAndStop(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	store := &benchmarkStore{run: benchmarkRun{
		State:        "running",
		Cancellation: cancel,
		Tasks: []benchTaskResult{
			{benchTask: benchTask{ID: "bugfix", Category: "coding"}, Failures: []string{"original"}},
		},
	}}
	for i := 0; i < 510; i++ {
		store.logf("line %d", i)
	}
	store.update(func(run *benchmarkRun) {
		run.Tasks[0].NetScore = 0.5
	})
	snapshot := store.snapshot()
	if snapshot.State != "running" || snapshot.Average != 0.3 || len(snapshot.Log) != 500 {
		t.Fatalf("snapshot=%#v", snapshot)
	}
	snapshot.Log[0] = "mutated"
	snapshot.Tasks[0].Failures[0] = "mutated"
	again := store.snapshot()
	if strings.Contains(again.Log[0], "mutated") || again.Tasks[0].Failures[0] != "original" {
		t.Fatalf("snapshot leaked mutable state: %#v", again)
	}
	if !store.stop() || store.snapshot().State != "cancelled" {
		t.Fatal("stop did not transition run")
	}
	select {
	case <-ctx.Done():
	default:
		t.Fatal("stop did not cancel context")
	}
}

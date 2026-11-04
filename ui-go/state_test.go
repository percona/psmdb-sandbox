package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"
)

func TestUpdateStateConcurrentTransactions(t *testing.T) {
	useTempState(t)
	const workers = 30
	var wg sync.WaitGroup
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			if err := updateState(func(state map[string]*Environment) error {
				state[fmt.Sprintf("env%d", i)] = &Environment{Status: "running"}
				if state["history"] == nil {
					state["history"] = &Environment{}
				}
				state["history"].History = append(state["history"].History, HistoryEvent{Action: "deploy"})
				return nil
			}); err != nil {
				t.Errorf("update: %v", err)
			}
		}(i)
	}
	wg.Wait()
	state, err := loadState()
	if err != nil {
		t.Fatal(err)
	}
	if len(state) != workers+1 || len(state["history"].History) != workers {
		t.Fatalf("lost updates: environments=%d history=%d", len(state), len(state["history"].History))
	}
	info, err := os.Stat(stateFile)
	if err != nil || info.Mode().Perm() != 0600 {
		t.Fatalf("registry permissions: %v %v", info, err)
	}
}

func TestUpdateStateFailurePreservesRegistry(t *testing.T) {
	useTempState(t)
	if err := saveState(map[string]*Environment{"keep": {Status: "running"}}); err != nil {
		t.Fatal(err)
	}
	before, _ := os.ReadFile(stateFile)
	want := errors.New("abort")
	if err := updateState(func(state map[string]*Environment) error {
		delete(state, "keep")
		return want
	}); !errors.Is(err, want) {
		t.Fatalf("expected callback error, got %v", err)
	}
	after, _ := os.ReadFile(stateFile)
	if string(after) != string(before) {
		t.Fatal("failed transaction changed registry")
	}
	for _, corrupt := range []string{"{", "null"} {
		if err := os.WriteFile(stateFile, []byte(corrupt), 0600); err != nil {
			t.Fatal(err)
		}
		if err := updateState(func(map[string]*Environment) error { t.Error("callback ran on corrupt registry"); return nil }); err == nil {
			t.Fatal("accepted corrupt registry")
		}
		after, _ = os.ReadFile(stateFile)
		if string(after) != corrupt {
			t.Fatal("overwrote corrupt registry")
		}
	}
}

func TestAtomicWriteFailureCleansTemporaryFile(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "directory")
	if err := os.Mkdir(target, 0700); err != nil {
		t.Fatal(err)
	}
	if err := atomicWriteFile(target, []byte("data"), 0600); err == nil {
		t.Fatal("expected rename failure")
	}
	files, _ := os.ReadDir(dir)
	if len(files) != 1 || files[0].Name() != "directory" {
		t.Fatalf("temporary files left behind: %v", files)
	}
}

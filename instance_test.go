package main

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestSingleInstanceConcurrentLaunches(t *testing.T) {
	path := filepath.Join(t.TempDir(), "Pi-Manager", "instance.lock")
	const launches = 16
	var group sync.WaitGroup
	results := make(chan *os.File, launches)
	start := make(chan struct{})
	for i := 0; i < launches; i++ {
		group.Add(1)
		go func() {
			defer group.Done()
			<-start
			file, err := lockSingleInstance(path)
			if err != nil {
				t.Error(err)
			}
			results <- file
		}()
	}
	close(start)
	group.Wait()
	close(results)
	owners := 0
	for file := range results {
		if file != nil {
			owners++
			if err := file.Close(); err != nil {
				t.Error(err)
			}
		}
	}
	if owners != 1 {
		t.Fatalf("%d simultaneous launches acquired ownership; want 1", owners)
	}
	// The metadata intentionally remains, but it must not block a later launch.
	file, err := lockSingleInstance(path)
	if err != nil || file == nil {
		t.Fatalf("restarting after normal exit: file=%v, error=%v", file, err)
	}
	file.Close()
}

func TestSingleInstanceProcessExit(t *testing.T) {
	path := filepath.Join(t.TempDir(), "instance.lock")
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	command := func() *exec.Cmd {
		cmd := exec.CommandContext(ctx, executable, "-test.run=^TestSingleInstanceHelper$")
		cmd.Env = append(os.Environ(), "PI_MANAGER_INSTANCE_TEST_PATH="+path)
		return cmd
	}
	owner := command()
	stdout, err := owner.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := owner.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = owner.Process.Kill(); _ = owner.Wait() })
	line, err := bufio.NewReader(stdout).ReadString('\n')
	if err != nil || line != "owner\n" {
		t.Fatalf("owner did not start: %q, %v", line, err)
	}
	output, err := command().CombinedOutput()
	if err != nil || !strings.Contains(string(output), "already running") {
		t.Fatalf("second process was not rejected: %s, %v", output, err)
	}
	if err := owner.Process.Kill(); err != nil {
		t.Fatal(err)
	}
	_ = owner.Wait()
	file, err := lockSingleInstance(path)
	if err != nil || file == nil {
		t.Fatalf("restarting after forced process termination: file=%v, error=%v", file, err)
	}
	file.Close()
}

func TestSingleInstanceHelper(t *testing.T) {
	path := os.Getenv("PI_MANAGER_INSTANCE_TEST_PATH")
	if path == "" {
		t.Skip("subprocess helper")
	}
	file, err := lockSingleInstance(path)
	if err != nil {
		t.Fatal(err)
	}
	if file == nil {
		fmt.Println("already running")
		return
	}
	defer file.Close()
	fmt.Println("owner")
	// The parent kills this helper to verify cleanup without running defers.
	time.Sleep(time.Minute)
}

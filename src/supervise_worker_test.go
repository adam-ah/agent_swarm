package main

import (
	"os"
	"os/exec"
	"strconv"
	"syscall"
	"testing"
	"time"
)

func named(command string) []int {
	var pids []int
	for pid, p := range scanProcs() {
		if p.command == command {
			pids = append(pids, pid)
		}
	}
	return pids
}

func killOnCleanup(t *testing.T, commands ...string) {
	t.Helper()
	t.Cleanup(func() {
		for _, c := range commands {
			for _, pid := range named(c) {
				_ = syscall.Kill(pid, syscall.SIGKILL)
			}
		}
	})
}

func supervisedChain(t *testing.T, project string, steps string, tools map[string]string) {
	t.Helper()
	files := map[string]string{".swarm/chains/supervised.json": `{"title": "Supervised", "steps": ` + steps + `}`}
	for name, run := range tools {
		files[".swarm/tools/"+name+".json"] = `{"title": "` + name + `", "run": "` + run + `"}`
	}
	writeFiles(t, project, files)
}

func TestWorkerStopsDetachedOrphansWithNoMarks(t *testing.T) {
	core, project := engineSetup(t)
	killOnCleanup(t, "sleep 3631")
	supervisedChain(t, project, `[{"id": "go", "title": "Go", "tool": "detach"}]`,
		map[string]string{"detach": "env -i setsid sleep 3631 > /dev/null 2>&1 & " + untilRunning("sleep 3631") + "echo detached"})
	run, err := core.StartRun(project, "supervised", nil)
	if err != nil {
		t.Fatal(err)
	}
	done := awaitRunEnd(t, core, run.ID)
	if done.Status != "done" {
		t.Fatalf("run = %s (%s)", done.Status, done.Reason)
	}
	if left := named("sleep 3631"); len(left) > 0 {
		t.Errorf("the orphan with a cleared environment is still running: %v", left)
	}
	if call := recordedCall(t, core.store.runDir(run.ID), "go"); !contains(call.Stopped, "sleep 3631") {
		t.Errorf("stopped %v", call.Stopped)
	}
}

func TestAWorkerThatDiesLeavesNothingBehind(t *testing.T) {
	core, project := engineSetup(t)
	killOnCleanup(t, "sleep 3632", "sleep 3633")
	supervisedChain(t, project, `[{"id": "hang", "title": "Hang", "tool": "hang"}]`,
		map[string]string{"hang": "sleep 3632 & sleep 3633"})
	run, err := core.StartRun(project, "supervised", nil)
	if err != nil {
		t.Fatal(err)
	}
	live := awaitRun(t, core, run.ID, func(r *Run) bool { return r.PID != 0 && len(named("sleep 3632")) > 0 && len(named("sleep 3633")) > 0 })
	if err := syscall.Kill(live.PID, syscall.SIGKILL); err != nil {
		t.Fatal(err)
	}
	awaitRun(t, core, run.ID, func(r *Run) bool { return r.Status == "interrupted" })
	stopped := core.store.sweep()
	if !contains(stopped, "sleep 3632") || !contains(stopped, "sleep 3633") {
		t.Errorf("swept %v", stopped)
	}
	if left := append(named("sleep 3632"), named("sleep 3633")...); len(left) > 0 {
		t.Errorf("still running after the sweep: %v", left)
	}
}

func TestAWorkerToldToStopTwiceStopsEverything(t *testing.T) {
	core, project := engineSetup(t)
	killOnCleanup(t, "sleep 3634", "sleep 3635")
	supervisedChain(t, project, `[{"id": "work", "title": "Work", "tool": "work"}, {"id": "tidy", "title": "Tidy", "tool": "tidy", "always": true}]`,
		map[string]string{"work": "sleep 3634", "tidy": "sleep 3635"})
	run, err := core.StartRun(project, "supervised", nil)
	if err != nil {
		t.Fatal(err)
	}
	live := awaitRun(t, core, run.ID, func(r *Run) bool { return r.PID != 0 && len(named("sleep 3634")) > 0 })
	if err := syscall.Kill(live.PID, syscall.SIGTERM); err != nil {
		t.Fatal(err)
	}
	for deadline := time.Now().Add(2 * time.Second); len(named("sleep 3635")) == 0; time.Sleep(5 * time.Millisecond) {
		if time.Now().After(deadline) {
			seen := core.store.load(run.ID)
			t.Fatalf("tidy never ran: %s %q, work %+v, tidy %+v", seen.Status, seen.Reason, *seen.Steps[0], *seen.Steps[1])
		}
	}
	if err := syscall.Kill(live.PID, syscall.SIGTERM); err != nil {
		t.Fatal(err)
	}
	ended := awaitRunEnd(t, core, run.ID)
	if ended.Status != "stopped" || !contains(ended.Stopped, "when the worker was told to stop a second time: ") || !contains(ended.Stopped, "sleep 3635") {
		t.Errorf("run %s stopped %v; tidy %s %q", ended.Status, ended.Stopped, ended.Steps[1].Status, ended.Steps[1].Error)
	}
	if left := named("sleep 3635"); len(left) > 0 {
		t.Errorf("the cleanup step is still running: %v", left)
	}
}

func TestAdoptionSparesWorkersAndReapsZombies(t *testing.T) {
	r := &Runner{store: testStore(t), adopting: true}
	plain := exec.Command("sleep", "3636")
	worker := exec.Command("sleep", "3637")
	worker.Env = append(os.Environ(), workerVar+"elsewhere")
	for _, cmd := range []*exec.Cmd{plain, worker} {
		if err := cmd.Start(); err != nil {
			t.Fatal(err)
		}
	}
	t.Cleanup(func() {
		_ = worker.Process.Kill()
		_ = worker.Wait()
	})
	owned := r.stepOwned("no step")(scanProcs())
	if !owned[plain.Process.Pid] || owned[worker.Process.Pid] {
		t.Fatalf("owned %v: want the plain child and not the worker", owned)
	}
	_ = plain.Process.Kill()
	for deadline := time.Now().Add(2 * time.Second); len(zombieChildren()) == 0 && time.Now().Before(deadline); {
		time.Sleep(5 * time.Millisecond)
	}
	r.reapAdopted()
	if _, err := os.Stat("/proc/" + strconv.Itoa(plain.Process.Pid)); err == nil {
		t.Error("the killed child was not reaped")
	}
}

package agent

import (
	"testing"
)

func persistedStateBase() *TaskState {
	return &TaskState{
		Version:      taskStateVersion,
		Task:         "persisted state integrity",
		TaskHash:     taskHash("persisted state integrity"),
		Project:      "/tmp/project",
		State:        StateDiscover,
		Iteration:    1,
		ReadFiles:    map[string]string{},
		FileHashes:   map[string]string{},
		History:      []string{},
		ActionLedger: []ActionRecord{},
	}
}

func pendingWrite(tool, path, content string) *PendingAction {
	return &PendingAction{
		Tool: tool,
		Args: map[string]string{
			"path":    path,
			"content": content,
		},
	}
}

func TestPersistedStateIntegrityRejectsUnknownState(t *testing.T) {
	state := persistedStateBase()
	state.State = State("BROKEN")

	if err := validatePersistedStateIntegrity(state); err == nil {
		t.Fatal("unknown state must be rejected")
	}
}

func TestPersistedStateIntegrityAcceptsImplementPendingWrite(t *testing.T) {
	state := persistedStateBase()
	state.State = StateImplement
	state.PendingAction = pendingWrite("write_file", "math.pl", "fixed")

	if err := validatePersistedStateIntegrity(state); err != nil {
		t.Fatalf("IMPLEMENT pending write should be accepted: %v", err)
	}
}

func TestPersistedStateIntegrityAcceptsWriteRequiredPendingWrite(t *testing.T) {
	state := persistedStateBase()
	state.State = StateWriteReq
	state.PendingAction = pendingWrite("write_file", "math.pl", "fixed")

	if err := validatePersistedStateIntegrity(state); err != nil {
		t.Fatalf("WRITE_REQUIRED pending write should be accepted: %v", err)
	}
}

func TestPersistedStateIntegrityAcceptsRepairWritePendingAction(t *testing.T) {
	state := persistedStateBase()
	state.State = StateRepairWrite
	state.RepairPath = "math.pl"
	state.PendingAction = pendingWrite("repair_write", "math.pl", "fixed")

	if err := validatePersistedStateIntegrity(state); err != nil {
		t.Fatalf("REPAIR_WRITE pending action should be accepted: %v", err)
	}
}

func TestPersistedStateIntegrityRejectsWrongPendingToolState(t *testing.T) {
	state := persistedStateBase()
	state.State = StateRepairWrite
	state.RepairPath = "math.pl"
	state.PendingAction = pendingWrite("write_file", "math.pl", "fixed")

	if err := validatePersistedStateIntegrity(state); err == nil {
		t.Fatal("write_file pending action in REPAIR_WRITE must be rejected")
	}
}

func TestPersistedStateIntegrityRejectsUnknownPendingTool(t *testing.T) {
	state := persistedStateBase()
	state.State = StateWriteReq
	state.PendingAction = pendingWrite("exec", "math.pl", "fixed")

	if err := validatePersistedStateIntegrity(state); err == nil {
		t.Fatal("unknown pending tool must be rejected")
	}
}

func TestPersistedStateIntegrityRejectsPendingWithoutPath(t *testing.T) {
	state := persistedStateBase()
	state.State = StateWriteReq
	state.PendingAction = pendingWrite("write_file", "", "fixed")

	if err := validatePersistedStateIntegrity(state); err == nil {
		t.Fatal("pending write without path must be rejected")
	}
}

func TestPersistedStateIntegrityRejectsPendingWithoutContent(t *testing.T) {
	state := persistedStateBase()
	state.State = StateWriteReq
	state.PendingAction = pendingWrite("write_file", "math.pl", "")

	if err := validatePersistedStateIntegrity(state); err == nil {
		t.Fatal("pending write without content must be rejected")
	}
}

func TestPersistedStateIntegrityRejectsRepairStateWithoutRepairPath(t *testing.T) {
	for _, stateName := range []State{StateRepairRead, StateRepairWrite} {
		state := persistedStateBase()
		state.State = stateName

		if err := validatePersistedStateIntegrity(state); err == nil {
			t.Fatalf("%s without RepairPath must be rejected", stateName)
		}
	}
}

func TestPersistedStateIntegrityRejectsPendingActionAfterWritePhase(t *testing.T) {
	for _, stateName := range []State{
		StateVerifyDiff,
		StateTest,
		StateRepairRead,
		StateDone,
	} {
		state := persistedStateBase()
		state.State = stateName

		if stateName == StateRepairRead {
			state.RepairPath = "math.pl"
		}

		state.PendingAction = pendingWrite("write_file", "math.pl", "fixed")

		if err := validatePersistedStateIntegrity(state); err == nil {
			t.Fatalf("%s with pending action must be rejected", stateName)
		}
	}
}

func TestPersistedStateIntegrityAcceptsValidDone(t *testing.T) {
	state := persistedStateBase()
	state.State = StateDone
	state.Wrote = true
	state.LastWritePath = "math.pl"

	state.ActionLedger = []ActionRecord{
		completionAction(1, "inspect_project", "math.pl"),
		completionAction(2, "read_file", "content", "math.pl"),
		completionAction(3, "write_file", "written", "math.pl", "fixed"),
		completionAction(4, "git_diff", "diff"),
		completionAction(5, "test", "PASS\n", "test.pl"),
	}

	if err := validatePersistedStateIntegrity(state); err != nil {
		t.Fatalf("valid DONE state rejected: %v", err)
	}
}

func TestPersistedStateIntegrityAllowsCrashSnapshotPendingWrite(t *testing.T) {
	state := persistedStateBase()
	state.State = StateImplement
	state.PendingAction = pendingWrite("write_file", "math.pl", "fixed")

	if err := validatePersistedStateIntegrity(state); err != nil {
		t.Fatalf("valid crash snapshot rejected: %v", err)
	}
}

func TestPersistedStateIntegrityAllowsRepairWriteCrashSnapshot(t *testing.T) {
	state := persistedStateBase()
	state.State = StateRepairWrite
	state.RepairPath = "math.pl"
	state.PendingAction = pendingWrite("repair_write", "math.pl", "fixed")

	if err := validatePersistedStateIntegrity(state); err != nil {
		t.Fatalf("valid repair crash snapshot rejected: %v", err)
	}
}

func TestPersistedStateIntegrityDoesNotRequireFullWorkflowHistory(t *testing.T) {
	state := persistedStateBase()
	state.State = StateVerifyDiff
	state.Wrote = true
	state.LastWritePath = "math.pl"

	if err := validatePersistedStateIntegrity(state); err != nil {
		t.Fatalf("partial VERIFY_DIFF snapshot should be accepted: %v", err)
	}
}

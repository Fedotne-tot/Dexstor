package agent

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"bratan-go/internal/projectindex"
)

const taskStateVersion = 3

type ActionRecord struct {
	Iteration int      `json:"iteration"`
	Tool      string   `json:"tool"`
	ActionKey string   `json:"action_key,omitempty"`
	Args      []string `json:"args,omitempty"`
	BaseHash  string   `json:"base_hash,omitempty"`
	Result    string   `json:"result"`
	Error     string   `json:"error,omitempty"`
	OK        bool     `json:"ok"`
	Exit      int      `json:"exit"`
}

type PendingAction struct {
	Tool     string            `json:"tool"`
	Args     map[string]string `json:"args,omitempty"`
	BaseHash string            `json:"base_hash,omitempty"`
}

type TaskState struct {
	Version      int                 `json:"version"`
	ProjectIndex *projectindex.Index `json:"project_index,omitempty"`

	Task     string `json:"task"`
	TaskHash string `json:"task_hash"`
	Project  string `json:"project"`

	State     State `json:"state"`
	Iteration int   `json:"iteration"`

	InspectTree   string `json:"inspect_tree"`
	ResumeContext string `json:"resume_context"`

	ReadFiles    map[string]string `json:"read_files"`
	FileHashes   map[string]string `json:"file_hashes"`
	ChangedFiles []string          `json:"changed_files"`

	LastWritePath string `json:"last_write_path"`
	LastTestError string `json:"last_test_error"`
	RepairPath    string `json:"repair_path"`

	LastAction string `json:"last_action"`
	LastResult string `json:"last_result"`

	PendingAction *PendingAction `json:"pending_action,omitempty"`

	History      []string       `json:"history"`
	ActionLedger []ActionRecord `json:"action_ledger"`

	Wrote       bool `json:"wrote"`
	DiffChecked bool `json:"diff_checked"`
}

func taskHash(task string) string {
	sum := sha256.Sum256([]byte(task))
	return hex.EncodeToString(sum[:])
}

func contentHash(content string) string {
	sum := sha256.Sum256([]byte(content))
	return hex.EncodeToString(sum[:])
}

func taskStatePath(root string) string {
	return filepath.Join(root, ".ai", "task_state.json")
}

func validateActionLedgerIntegrity(ledger []ActionRecord) error {
	for i, record := range ledger {
		// Legacy v2 records may have no ActionKey. They remain
		// loadable for backward compatibility, but are not treated
		// as modern integrity-verified actions elsewhere.
		if record.ActionKey == "" {
			continue
		}

		expected := makeActionKey(record.Tool, record.Args...)
		if expected != record.ActionKey {
			return fmt.Errorf(
				"action ledger integrity failure at entry %d: tool=%q action_key=%q expected=%q",
				i,
				record.Tool,
				record.ActionKey,
				expected,
			)
		}

		switch record.Tool {
		case "inspect_project", "git_diff":
			if len(record.Args) != 0 {
				return fmt.Errorf(
					"action ledger semantic failure at entry %d: tool=%q expects 0 args, got %d",
					i,
					record.Tool,
					len(record.Args),
				)
			}

		case "read_file", "test", "repair_read":
			if len(record.Args) != 1 {
				return fmt.Errorf(
					"action ledger semantic failure at entry %d: tool=%q expects 1 arg, got %d",
					i,
					record.Tool,
					len(record.Args),
				)
			}

			if record.Args[0] == "" {
				return fmt.Errorf(
					"action ledger semantic failure at entry %d: tool=%q has empty path argument",
					i,
					record.Tool,
				)
			}

		case "exec":
			if len(record.Args) != 1 {
				return fmt.Errorf(
					"action ledger semantic failure at entry %d: tool=%q expects 1 arg, got %d",
					i,
					record.Tool,
					len(record.Args),
				)
			}

			if record.Args[0] == "" {
				return fmt.Errorf(
					"action ledger semantic failure at entry %d: tool=%q has empty command argument",
					i,
					record.Tool,
				)
			}

		case "assert_output":
			if len(record.Args) != 2 {
				return fmt.Errorf(
					"action ledger semantic failure at entry %d: tool=%q expects 2 args, got %d",
					i,
					record.Tool,
					len(record.Args),
				)
			}

			if record.Args[0] == "" {
				return fmt.Errorf(
					"action ledger semantic failure at entry %d: tool=%q has empty command argument",
					i,
					record.Tool,
				)
			}

		case "write_file", "repair_write":
			if len(record.Args) != 2 {
				return fmt.Errorf(
					"action ledger semantic failure at entry %d: tool=%q expects 2 args, got %d",
					i,
					record.Tool,
					len(record.Args),
				)
			}

			if record.Args[0] == "" {
				return fmt.Errorf(
					"action ledger semantic failure at entry %d: tool=%q has empty path argument",
					i,
					record.Tool,
				)
			}

		default:
			return fmt.Errorf(
				"action ledger semantic failure at entry %d: unknown tool %q",
				i,
				record.Tool,
			)
		}
	}

	return nil
}

func validateActionLedgerTransitionIntegrity(ledger []ActionRecord) error {
	inspected := false
	read := false
	readPaths := make(map[string]string)

	written := false
	diffChecked := false

	testFailed := false

	repairRead := false
	repairReadPaths := make(map[string]string)

	for i, record := range ledger {
		if record.ActionKey == "" {
			continue
		}

		switch record.Tool {
		case "inspect_project":
			inspected = true

		case "read_file":
			read = true

			if len(record.Args) == 1 && record.Args[0] != "" {
				readPaths[record.Args[0]] = contentHash(record.Result)
			}

		case "exec":
			// exec is an execution/observation action. It does not
			// change the filesystem snapshot state by itself.
			continue

		case "assert_output":
			// assert_output is a verification action. It does not
			// change the filesystem snapshot state.
			continue

		case "write_file":
			if inspected && !read {
				return fmt.Errorf(
					"action ledger transition failure at entry %d: write_file after inspect_project requires read_file",
					i,
				)
			}

			if inspected && len(record.Args) >= 1 {
				path := record.Args[0]

				if path == "" {
					return fmt.Errorf(
						"action ledger transition failure at entry %d: write_file has empty path",
						i,
					)
				}

				expectedBaseHash, ok := readPaths[path]
				if !ok {
					return fmt.Errorf(
						"action ledger transition failure at entry %d: write_file path %q was not read",
						i,
						path,
					)
				}

				if record.BaseHash == "" {
					return fmt.Errorf(
						"action ledger transition failure at entry %d: write_file path %q has no snapshot anchor",
						i,
						path,
					)
				}

				if record.BaseHash != expectedBaseHash {
					return fmt.Errorf(
						"action ledger transition failure at entry %d: write_file path %q snapshot anchor mismatch",
						i,
						path,
					)
				}
			}

			written = true
			diffChecked = false

		case "git_diff":
			if !written {
				return fmt.Errorf(
					"action ledger transition failure at entry %d: git_diff before write_file",
					i,
				)
			}

			diffChecked = true

		case "test":
			if !diffChecked {
				return fmt.Errorf(
					"action ledger transition failure at entry %d: test before git_diff",
					i,
				)
			}

			if record.OK && record.Exit == 0 {
				if !strings.Contains(record.Result, "PASS") {
					return fmt.Errorf(
						"action ledger transition failure at entry %d: successful test without PASS",
						i,
					)
				}

				testFailed = false
				repairRead = false
				repairReadPaths = make(map[string]string)
			} else {
				testFailed = true
				repairRead = false
				repairReadPaths = make(map[string]string)
			}

		case "repair_read":
			if !testFailed {
				return fmt.Errorf(
					"action ledger transition failure at entry %d: repair_read requires a failed test",
					i,
				)
			}

			repairRead = true

			if len(record.Args) == 1 && record.Args[0] != "" {
				repairReadPaths[record.Args[0]] = contentHash(record.Result)
			}

		case "repair_write":
			if !repairRead {
				return fmt.Errorf(
					"action ledger transition failure at entry %d: repair_write requires repair_read",
					i,
				)
			}

			if len(record.Args) >= 1 {
				path := record.Args[0]

				if path == "" {
					return fmt.Errorf(
						"action ledger transition failure at entry %d: repair_write has empty path",
						i,
					)
				}

				expectedBaseHash, ok := repairReadPaths[path]
				if !ok {
					return fmt.Errorf(
						"action ledger transition failure at entry %d: repair_write path %q was not read by repair_read",
						i,
						path,
					)
				}

				if record.BaseHash == "" {
					return fmt.Errorf(
						"action ledger transition failure at entry %d: repair_write path %q has no snapshot anchor",
						i,
						path,
					)
				}

				if record.BaseHash != expectedBaseHash {
					return fmt.Errorf(
						"action ledger transition failure at entry %d: repair_write path %q snapshot anchor mismatch",
						i,
						path,
					)
				}
			}

			written = true
			diffChecked = false
			testFailed = false

		default:
			return fmt.Errorf(
				"action ledger transition failure at entry %d: unknown tool %q",
				i,
				record.Tool,
			)
		}
	}

	return nil
}

func validateActionLedgerResultIntegrity(ledger []ActionRecord) error {
	for i, record := range ledger {
		// Legacy v2 records without ActionKey remain loadable.
		if record.ActionKey == "" {
			continue
		}

		if record.OK && record.Exit != 0 {
			return fmt.Errorf(
				"action ledger result failure at entry %d: successful action has non-zero exit %d",
				i,
				record.Exit,
			)
		}

		if !record.OK && record.Exit == 0 {
			return fmt.Errorf(
				"action ledger result failure at entry %d: failed action has zero exit",
				i,
			)
		}

		if record.Tool == "test" && record.OK && record.Exit == 0 {
			if !strings.Contains(record.Result, "PASS") {
				return fmt.Errorf(
					"action ledger result failure at entry %d: successful test result does not contain PASS",
					i,
				)
			}
		}
	}

	return nil
}

func validateActionLedgerTemporalIntegrity(ledger []ActionRecord) error {
	lastIteration := 0

	for i, record := range ledger {
		// Legacy records without ActionKey remain loadable.
		if record.ActionKey == "" {
			continue
		}

		if record.Iteration < 1 {
			return fmt.Errorf(
				"action ledger temporal failure at entry %d: iteration must be >= 1, got %d",
				i,
				record.Iteration,
			)
		}

		if lastIteration > 0 && record.Iteration < lastIteration {
			return fmt.Errorf(
				"action ledger temporal failure at entry %d: iteration moved backward from %d to %d",
				i,
				lastIteration,
				record.Iteration,
			)
		}

		lastIteration = record.Iteration
	}

	return nil
}

func validateActionLedgerSnapshotIntegrity(state *TaskState) error {
	if state == nil {
		return fmt.Errorf("action ledger snapshot integrity failure: nil state")
	}

	if len(state.ReadFiles) == 0 {
		return nil
	}

	latestRead := make(map[string]string)

	for _, record := range state.ActionLedger {
		if record.ActionKey == "" {
			continue
		}

		switch record.Tool {
		case "read_file", "repair_read":
			if len(record.Args) != 1 || record.Args[0] == "" {
				continue
			}

			path := record.Args[0]
			latestRead[path] = record.Result
		}
	}

	for path, ledgerContent := range latestRead {
		stateContent, ok := state.ReadFiles[path]
		if !ok {
			return fmt.Errorf(
				"action ledger snapshot integrity failure: %s at path %q has no persisted ReadFiles snapshot",
				"read result",
				path,
			)
		}

		if ledgerContent != stateContent {
			return fmt.Errorf(
				"action ledger snapshot integrity failure: path %q read result does not match persisted snapshot",
				path,
			)
		}
	}

	return nil
}

func validatePersistedStateIntegrity(state *TaskState) error {
	if state == nil {
		return fmt.Errorf("persisted state integrity failure: nil state")
	}

	switch state.State {
	case StateDiscover,
		StateImplement,
		StateWriteReq,
		StateVerifyDiff,
		StateTest,
		StateRepairRead,
		StateRepairWrite,
		StateDone:
		// valid persisted FSM state

	default:
		return fmt.Errorf(
			"persisted state integrity failure: unknown state %q",
			state.State,
		)
	}

	if state.PendingAction != nil {
		pending := state.PendingAction

		switch pending.Tool {
		case "write_file":
			if state.State != StateImplement &&
				state.State != StateWriteReq {
				return fmt.Errorf(
					"persisted state integrity failure: write_file pending action in state %s",
					state.State,
				)
			}

		case "repair_write":
			if state.State != StateRepairWrite {
				return fmt.Errorf(
					"persisted state integrity failure: repair_write pending action in state %s",
					state.State,
				)
			}

		default:
			return fmt.Errorf(
				"persisted state integrity failure: unknown pending tool %q",
				pending.Tool,
			)
		}

		if pending.Args == nil {
			return fmt.Errorf(
				"persisted state integrity failure: pending %s has nil args",
				pending.Tool,
			)
		}

		if pending.Args["path"] == "" {
			return fmt.Errorf(
				"persisted state integrity failure: pending %s has empty path",
				pending.Tool,
			)
		}

		if pending.Args["content"] == "" {
			return fmt.Errorf(
				"persisted state integrity failure: pending %s has empty content",
				pending.Tool,
			)
		}
	}

	switch state.State {
	case StateVerifyDiff, StateTest, StateRepairRead, StateDone:
		if state.PendingAction != nil {
			return fmt.Errorf(
				"persisted state integrity failure: state %s cannot have pending action",
				state.State,
			)
		}
	}

	switch state.State {
	case StateRepairRead, StateRepairWrite:
		if state.RepairPath == "" {
			return fmt.Errorf(
				"persisted state integrity failure: state %s requires repair_path",
				state.State,
			)
		}
	}

	return nil
}

func loadTaskState(root, task string) (*TaskState, bool, error) {
	path := taskStatePath(root)

	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, false, nil
		}

		return nil, false, err
	}

	var state TaskState

	if err := json.Unmarshal(data, &state); err != nil {
		corruptPath := path + ".corrupt"

		if _, statErr := os.Stat(corruptPath); statErr == nil {
			for i := 1; ; i++ {
				candidate := fmt.Sprintf(
					"%s.%d",
					corruptPath,
					i,
				)

				if _, candidateErr := os.Stat(candidate); os.IsNotExist(candidateErr) {
					corruptPath = candidate
					break
				}
			}
		} else if !os.IsNotExist(statErr) {
			return nil, false, fmt.Errorf(
				"inspect corrupted task state backup: %w",
				statErr,
			)
		}

		if renameErr := os.Rename(path, corruptPath); renameErr != nil {
			return nil, false, fmt.Errorf(
				"backup corrupted task state %s: %w",
				path,
				renameErr,
			)
		}

		return nil, false, nil
	}

	if state.Version != taskStateVersion {
		if state.Version != 2 {
			return nil, false, fmt.Errorf(
				"unsupported task state version: %d",
				state.Version,
			)
		}

		// Migrate the persisted v2 format to the current v3 format.
		// v2 did not have ProjectIndex and ActionRecord did not persist
		// OK/Exit fields. Existing v2 ledger entries represent completed
		// actions, so restore their successful execution status explicitly.
		state.Version = taskStateVersion

		for i := range state.ActionLedger {
			if state.ActionLedger[i].Error == "" {
				state.ActionLedger[i].OK = true
				state.ActionLedger[i].Exit = 0
			}
		}

		if err := saveTaskState(root, &state); err != nil {
			return nil, false, fmt.Errorf(
				"migrate task state %s: %w",
				path,
				err,
			)
		}
	}

	if err := validateActionLedgerIntegrity(state.ActionLedger); err != nil {
		return nil, false, err
	}

	if err := validateActionLedgerTemporalIntegrity(state.ActionLedger); err != nil {
		return nil, false, err
	}

	if err := validateActionLedgerResultIntegrity(state.ActionLedger); err != nil {
		return nil, false, err
	}

	if err := validateActionLedgerTransitionIntegrity(state.ActionLedger); err != nil {
		return nil, false, err
	}

	if err := validateActionLedgerSnapshotIntegrity(&state); err != nil {
		return nil, false, err
	}

	if err := validatePersistedStateIntegrity(&state); err != nil {
		return nil, false, err
	}

	if state.TaskHash != taskHash(task) {
		return nil, false, nil
	}

	if state.ReadFiles == nil {
		state.ReadFiles = make(map[string]string)
	}

	if state.FileHashes == nil {
		state.FileHashes = make(map[string]string)
	}

	if state.ChangedFiles == nil {
		state.ChangedFiles = make([]string, 0)
	}

	if state.History == nil {
		state.History = make([]string, 0)
	}

	return &state, true, nil
}

func saveTaskState(root string, state *TaskState) error {
	path := taskStatePath(root)
	dir := filepath.Dir(path)

	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf(
			"create task state directory: %w",
			err,
		)
	}

	data, err := json.MarshalIndent(
		state,
		"",
		"  ",
	)

	if err != nil {
		return fmt.Errorf(
			"marshal task state: %w",
			err,
		)
	}

	tmp := path + ".tmp"

	if err := os.WriteFile(
		tmp,
		data,
		0644,
	); err != nil {
		return fmt.Errorf(
			"write temporary task state: %w",
			err,
		)
	}

	if err := os.Rename(tmp, path); err != nil {
		return fmt.Errorf(
			"replace task state: %w",
			err,
		)
	}

	return nil
}

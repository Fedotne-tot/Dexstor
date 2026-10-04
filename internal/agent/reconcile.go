package agent

import (
	"fmt"
	"os"
	"path/filepath"
)

func (a *Agent) reconcile(state *TaskState) (bool, error) {
	fmt.Println("[BRATAN GO] RECONCILE: checking filesystem")

	/*
		If the process crashed while a write was in flight,
		PendingAction is authoritative for the recovery decision.

		Only an exact filesystem match means that the physical write
		completed and the pending action can be recovered.

		If the filesystem does not contain the pending content, the
		pending write is NOT considered completed. We must not fall
		through to FileHashes because those hashes describe the older
		persisted snapshot.
	*/
	if state.PendingAction != nil {
		pending := state.PendingAction

		if pending.Tool == "write_file" || pending.Tool == "repair_write" {
			path := pending.Args["path"]
			content := pending.Args["content"]

			if path == "" {
				return false, nil
			}

			fullPath := filepath.Join(
				a.Root,
				filepath.Clean(path),
			)

			data, err := os.ReadFile(fullPath)
			if err == nil {
				if string(data) == content {
					fmt.Printf(
						"[BRATAN GO] RECONCILE: pending write already completed %s\n",
						path,
					)
					return true, nil
				}

				fmt.Printf(
					"[BRATAN GO] RECONCILE: pending write not completed %s\n",
					path,
				)
				return false, nil
			}

			if os.IsNotExist(err) {
				fmt.Printf(
					"[BRATAN GO] RECONCILE: pending write target missing %s\n",
					path,
				)
				return false, nil
			}

			return false, fmt.Errorf(
				"reconcile pending write %s: %w",
				path,
				err,
			)
		}
	}

	for path, expectedHash := range state.FileHashes {
		fullPath := filepath.Join(a.Root, filepath.Clean(path))

		data, err := os.ReadFile(fullPath)
		if err != nil {
			if os.IsNotExist(err) {
				fmt.Printf(
					"[BRATAN GO] RECONCILE: missing file=%s\n",
					path,
				)
				return false, nil
			}

			return false, fmt.Errorf(
				"reconcile read %s: %w",
				path,
				err,
			)
		}

		actualHash := contentHash(string(data))

		if actualHash != expectedHash {
			fmt.Printf(
				"[BRATAN GO] RECONCILE: changed file=%s\n",
				path,
			)
			return false, nil
		}

		fmt.Printf(
			"[BRATAN GO] RECONCILE: verified %s\n",
			path,
		)
	}

	fmt.Println("[BRATAN GO] RECONCILE: filesystem matches saved state")

	return true, nil
}

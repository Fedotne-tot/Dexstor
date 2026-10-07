# DEXSTER Local Model Benchmark

Benchmark of local Ollama models using DEXSTER.

## Test

All models were tested on the same existing-file task.

Task:

- modify `index.html`
- change `Original Title` to `DEXSTER Construction`
- preserve the existing paragraph
- keep valid HTML
- do not create another HTML file

The DEXSTER controller enforces a state machine:

`inspect → read → write → diff → test`

Models cannot directly execute arbitrary tool calls outside the controller protocol.

## Results

| Model | Result | Iterations | Notes |
|---|---:|---:|---|
| qwen3-coder:30b | PASS | 5 | Clean protocol |
| deepseek-coder-v2:16b | PASS | 8 | Protocol noise |
| bratan-coder:latest | PASS | 5 | Clean |
| bratan-deepseek:latest | PASS | 8 | Protocol noise |
| qwen3.8:27b | PASS | 5 | Clean |
| qwen3:8b | PASS | 5 | Clean |
| qwen3-vl:8b | PASS | 5 | 1 NUDGE |
| qwen-custom:latest | PASS | 5 | Clean |
| ministral-3:14b | PASS | 5 | Markdown tool-call formatting |
| devstral-small-2:latest | PASS | 5 | First attempt OOM, second PASS |
| gemma4:latest | PASS | 5 | Clean |
| gemma3:4b | FAIL | 10 | Invalid tool protocol and failed repair |
| gpt-oss:20b | FAIL | 5+ | Empty responses in IMPLEMENT |
| gemma3:1b | FAIL | 7 | Wrong tool protocol |

## Summary

11 of 14 tested models completed the task successfully.

The failed models did not bypass the DEXSTER controller.

### gemma3:4b

The model generated fenced/fake tool calls instead of the required DEXSTER protocol. It also produced incorrect HTML during repair.

DEXSTER entered `REPAIR_WRITE` and stopped after repeated invalid tool-call responses.

### gpt-oss:20b

The model repeatedly returned an empty response in `IMPLEMENT`.

After three nudges without a valid `TOOL_CALL`, DEXSTER stopped safely.

The test was repeated and produced the same behavior.

### gemma3:1b

The model generated a different tool protocol:

`tool_code`

and attempted actions such as:

`WRITE_FILE main.go`

instead of operating on the requested `index.html`.

DEXSTER rejected the responses and stopped after three nudges.

## Important Observation

The benchmark measures not only whether a model can modify a file, but whether it can operate through the DEXSTER tool protocol.

A model failure does not automatically become an agent failure.

The controller enforces:

- valid tool-call parsing
- FSM state transitions
- trusted read snapshots
- write requirements
- change verification
- test requirements
- repair states
- bounded retries
- persistent state recovery

This allows unreliable models to fail safely without gaining uncontrolled filesystem access.

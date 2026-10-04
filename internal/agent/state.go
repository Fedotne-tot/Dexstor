package agent

type State string

const (
	StateDiscover    State = "DISCOVER"
	StateImplement   State = "IMPLEMENT"
	StateWriteReq    State = "WRITE_REQUIRED"
	StateVerifyDiff  State = "VERIFY_DIFF"
	StateTest        State = "TEST"
	StateRepairRead  State = "REPAIR_READ"
	StateRepairWrite State = "REPAIR_WRITE"
	StateDone        State = "DONE"
)

package sparkplug

import "sync/atomic"

// State represents the Sparkplug B edge node lifecycle state.
type State int32

const (
	Offline    State = 0
	Connecting State = 1
	Online     State = 2
)

func (s State) String() string {
	switch s {
	case Offline:
		return "OFFLINE"
	case Connecting:
		return "CONNECTING"
	case Online:
		return "ONLINE"
	default:
		return "UNKNOWN"
	}
}

// Event triggers a state transition.
type Event int

const (
	EventConnectAttempt Event = iota
	EventConnectSuccess
	EventConnectFail
	EventDisconnect
)

// StateMachine manages the Sparkplug B edge node state.
// Safe for concurrent use — state is stored atomically.
type StateMachine struct {
	state atomic.Int32
}

// State returns the current state without locking.
func (sm *StateMachine) State() State {
	return State(sm.state.Load())
}

// Transition applies an event to the state machine. Invalid transitions
// are silently ignored (no-op, not an error).
func (sm *StateMachine) Transition(event Event) {
	for {
		current := State(sm.state.Load())
		next, ok := transition(current, event)
		if !ok {
			return
		}
		if sm.state.CompareAndSwap(int32(current), int32(next)) {
			return
		}
	}
}

func transition(from State, event Event) (State, bool) {
	switch {
	case from == Offline && event == EventConnectAttempt:
		return Connecting, true
	case from == Connecting && event == EventConnectSuccess:
		return Online, true
	case from == Connecting && event == EventConnectFail:
		return Offline, true
	case from == Online && event == EventDisconnect:
		return Offline, true
	default:
		return from, false
	}
}

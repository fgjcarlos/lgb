package sparkplug_test

import (
	"sync"
	"testing"

	"github.com/fgjcarlos/lgb/internal/sparkplug"
)

func TestStateMachine_SuccessfulConnect(t *testing.T) {
	t.Parallel()
	var sm sparkplug.StateMachine
	if sm.State() != sparkplug.Offline {
		t.Fatalf("initial state = %v; want Offline", sm.State())
	}
	sm.Transition(sparkplug.EventConnectAttempt)
	if sm.State() != sparkplug.Connecting {
		t.Errorf("after ConnectAttempt = %v; want Connecting", sm.State())
	}
	sm.Transition(sparkplug.EventConnectSuccess)
	if sm.State() != sparkplug.Online {
		t.Errorf("after ConnectSuccess = %v; want Online", sm.State())
	}
}

func TestStateMachine_FailedConnect(t *testing.T) {
	t.Parallel()
	var sm sparkplug.StateMachine
	sm.Transition(sparkplug.EventConnectAttempt)
	sm.Transition(sparkplug.EventConnectFail)
	if sm.State() != sparkplug.Offline {
		t.Errorf("after ConnectFail = %v; want Offline", sm.State())
	}
}

func TestStateMachine_InvalidTransitionIgnored(t *testing.T) {
	t.Parallel()
	var sm sparkplug.StateMachine
	sm.Transition(sparkplug.EventConnectSuccess)
	if sm.State() != sparkplug.Offline {
		t.Errorf("invalid ConnectSuccess from Offline = %v; want Offline", sm.State())
	}
}

func TestStateMachine_Disconnect(t *testing.T) {
	t.Parallel()
	var sm sparkplug.StateMachine
	sm.Transition(sparkplug.EventConnectAttempt)
	sm.Transition(sparkplug.EventConnectSuccess)
	sm.Transition(sparkplug.EventDisconnect)
	if sm.State() != sparkplug.Offline {
		t.Errorf("after Disconnect = %v; want Offline", sm.State())
	}
}

func TestStateMachine_ConcurrentReads(t *testing.T) {
	t.Parallel()
	var sm sparkplug.StateMachine
	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = sm.State()
		}()
	}
	wg.Wait()
}

package telemetry

import "github.com/smartcontractkit/chainlink/core/services/synchronization"

type Agent struct {
	wsclient synchronization.WebSocketClient
}

// NewAgent returns a Agent which is just a thin wrapper over
// the wsclient for now
func NewAgent(wsclient synchronization.WebSocketClient) *Agent {
	return &Agent{wsclient}
}

// SendLog sends a telemetry log to the explorer
func (t *Agent) SendLog(log []byte) {
	t.wsclient.Send(log)
}

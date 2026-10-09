package shell

import (
	"context"
	"fmt"
	"log"

	"github.com/Nomadcxx/sysc-notify/protocol"

	"github.com/Nomadcxx/sysc-shell/internal/services/polkit"
)

// configurePolkit runs outside Registry.mu because stopping an agent waits for
// its D-Bus call and request relay to finish.
func (r *Registry) configurePolkit() {
	if r == nil || runningAsTest() {
		return
	}
	r.polkitLifecycleMu.Lock()
	defer r.polkitLifecycleMu.Unlock()
	select {
	case <-r.closed:
		return
	default:
	}

	r.mu.Lock()
	policy := r.cfg.Session.PolkitAgent
	if policy == "" {
		policy = polkit.PolicyAuto
	}
	if r.polkitAgent != nil && r.polkitAgent.Status().Policy == policy {
		r.mu.Unlock()
		return
	}
	r.mu.Unlock()
	r.stopPolkitLocked()

	select {
	case <-r.closed:
		return
	default:
	}
	agent := polkit.New(polkit.Options{Policy: policy, Logf: log.Printf})
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	r.mu.Lock()
	select {
	case <-r.closed:
		r.mu.Unlock()
		cancel()
		return
	default:
	}
	r.polkitAgent, r.polkitCancel, r.polkitDone = agent, cancel, done
	agent.Hold(r.polkitLockerRunningLocked())
	r.mu.Unlock()
	go r.runPolkitAgent(ctx, cancel, agent, done)
	r.polkitChanged(agent)
}

// stopPolkitLocked requires polkitLifecycleMu.
func (r *Registry) stopPolkitLocked() {
	r.mu.Lock()
	cancel, done := r.polkitCancel, r.polkitDone
	r.polkitAgent, r.polkitCancel, r.polkitDone = nil, nil, nil
	if h := r.polkitHost; h != nil && h.open_ {
		h.finishLocked(true)
	}
	r.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	if done != nil {
		<-done
	}
}

func (r *Registry) runPolkitAgent(ctx context.Context, cancel context.CancelFunc, agent *polkit.Agent, done chan struct{}) {
	defer close(done)
	runResult := make(chan error, 1)
	go func() { runResult <- agent.Run(ctx) }()

	var (
		active      polkit.Request
		hasActive   bool
		prompts     <-chan polkit.Prompt
		requestDone <-chan struct{}
		presented   bool
		runErr      error
		runFinished bool
	)
	for {
		select {
		case <-ctx.Done():
			goto stopped
		case <-r.closed:
			goto stopped
		case runErr = <-runResult:
			runFinished = true
			goto stopped
		case req := <-agent.Requests():
			if hasActive {
				active.Cancel()
				r.finishPolkitPrompt(agent, active.Cookie)
			}
			active, hasActive = req, true
			prompts, requestDone = req.Prompts(), req.Done()
			presented = r.presentPolkitPrompt(agent, req)
		case prompt, ok := <-prompts:
			if !ok {
				prompts = nil
				continue
			}
			r.mu.Lock()
			if r.polkitAgent == agent && r.polkitHost != nil && r.polkitHost.open_ && r.polkitHost.request_.Cookie == active.Cookie {
				r.polkitHost.setPromptLocked(prompt)
			}
			r.mu.Unlock()
		case <-requestDone:
			if hasActive {
				r.finishPolkitPrompt(agent, active.Cookie)
			}
			active, hasActive = polkit.Request{}, false
			prompts, requestDone = nil, nil
			presented = false
		case cancelled := <-agent.Withdrawn():
			r.showPolkitCancellation(cancelled)
		case holder := <-agent.Notice():
			r.showPolkitAgentNotice(holder)
		case <-r.polkitOutputEvents:
			if hasActive && !presented {
				presented = r.presentPolkitPrompt(agent, active)
			}
		case <-agent.Changes():
			r.polkitChanged(agent)
		}
	}

stopped:
	wasStopped := ctx.Err() != nil
	cancel()
	if hasActive {
		active.Cancel()
		r.finishPolkitPrompt(agent, active.Cookie)
	}
	if !runFinished {
		runErr = <-runResult
	}
	r.polkitChanged(agent)
	if runErr != nil && !wasStopped {
		log.Printf("shell: polkit authentication agent: %v", runErr)
	}
}

func (r *Registry) presentPolkitPrompt(agent *polkit.Agent, req polkit.Request) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.polkitAgent != agent || r.polkitHost == nil {
		req.Cancel()
		return true
	}
	output, connector, ok := r.polkitOutputLocked()
	if !ok {
		return false
	}
	r.polkitHost.openLocked(req, output, connector, agent.Waiting())
	return true
}

func (r *Registry) signalPolkitOutputChange() {
	if r == nil {
		return
	}
	select {
	case r.polkitOutputEvents <- struct{}{}:
	default:
	}
}

func (r *Registry) showPolkitCancellation(cancelled polkit.CancelledRequest) {
	program := polkitCallerProgram(cancelled.Details["polkit.caller-pid"])
	r.showPolkitToast("Authentication request cancelled", fmt.Sprintf("Authentication request from %s was cancelled", program))
}

func (r *Registry) showPolkitAgentNotice(holder string) {
	if holder == "" {
		holder = "an unknown authentication agent"
	}
	r.showPolkitToast("Another authentication agent is active", "Using "+holder)
}

func (r *Registry) showPolkitToast(summary, body string) {
	if r == nil || r.producerSender == nil {
		return
	}
	key := fmt.Sprintf("sysc-shell:polkit:%d", r.pluginNotifySeq.Add(1))
	if _, err := r.publishToast(key, summary, body, protocol.UrgencyNormal, -1); err != nil {
		log.Printf("shell: polkit toast: %v", err)
	}
}

func (r *Registry) finishPolkitPrompt(agent *polkit.Agent, cookie string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.polkitAgent == agent && r.polkitHost != nil && r.polkitHost.open_ && r.polkitHost.request_.Cookie == cookie {
		r.polkitHost.finishLocked(false)
	}
}

func (r *Registry) polkitOutputLocked() (uint32, string, bool) {
	globals := r.outputGlobalsLocked()
	if global, ok := globals[r.focused]; ok {
		return global, r.focused, true
	}
	var selected uint32
	connector := ""
	for name, global := range globals {
		if connector == "" || global < selected {
			selected, connector = global, name
		}
	}
	return selected, connector, connector != ""
}

func (r *Registry) polkitChanged(agent *polkit.Agent) {
	r.mu.Lock()
	if r.polkitAgent != agent {
		r.mu.Unlock()
		return
	}
	if r.polkitHost != nil {
		r.polkitHost.refreshWaitingLocked(agent.Waiting())
	}
	output := uint32(0)
	if h := r.panelHosts[PanelSettings]; h != nil && h.section == "Session" {
		r.rebuildPanel(h)
		output = h.output
	}
	r.mu.Unlock()
	if output != 0 {
		r.publishSurface(output, panelSurfaceID(PanelSettings))
	}
}

func (r *Registry) polkitStatusLocked() polkit.Status {
	if r.polkitAgent != nil {
		return r.polkitAgent.Status()
	}
	policy := r.cfg.Session.PolkitAgent
	if policy == "" {
		policy = polkit.PolicyAuto
	}
	status := polkit.Status{Policy: policy}
	if policy == polkit.PolicyOff {
		status.Reason = "disabled"
	}
	return status
}

func (r *Registry) polkitHoldLocked(locked bool) {
	if r.polkitAgent != nil {
		r.polkitAgent.Hold(locked)
	}
}

func (r *Registry) polkitLockerRunningLocked() bool {
	if r.managedLock != nil && isManagedLocker(sessionArgv("session-lock", r.cfg.Session.Locker)) {
		phase := r.managedState.Phase
		return phase != "idle" && phase != "failed-before-acquisition" && phase != "unavailable"
	}
	return r.lockerRunning
}

func (r *Registry) syncPolkitLockHold() {
	r.mu.Lock()
	r.polkitHoldLocked(r.polkitLockerRunningLocked())
	r.mu.Unlock()
}

package services

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os/exec"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	privacyPollInterval = 500 * time.Millisecond
	maxPrivacyDumpBytes = 16 << 20
)

// PrivacyVideoPath is one active video source-to-capture-stream path. Camera
// is true only when structural PipeWire device metadata identifies a camera.
// Other paths retain their source IDs for correlation with Niri casts.
type PrivacyVideoPath struct {
	SourceNodeID uint32
	Camera       bool
}

// PrivacySnapshot is the PipeWire portion of privacy state. Known is false
// until a complete graph snapshot has been parsed successfully. Unclassified
// audio capture is kept separate from positive microphone evidence so shell
// state can prefer an active proof while still reporting ambiguity otherwise.
type PrivacySnapshot struct {
	Known             bool
	MicrophoneActive  bool
	MicrophoneUnknown bool
	VideoPaths        []PrivacyVideoPath
}

// Clone returns an immutable copy suitable for crossing the services/shell
// boundary.
func (s PrivacySnapshot) Clone() PrivacySnapshot {
	s.VideoPaths = slices.Clone(s.VideoPaths)
	return s
}

type privacyDumpInfo struct {
	State        *string        `json:"state"`
	Props        map[string]any `json:"props"`
	OutputNodeID *uint32        `json:"output-node-id"`
	InputNodeID  *uint32        `json:"input-node-id"`
}

type privacyDumpEntry struct {
	ID    *uint32          `json:"id"`
	Type  string           `json:"type"`
	Props map[string]any   `json:"props"`
	Info  *privacyDumpInfo `json:"info"`
}

func (e privacyDumpEntry) props() map[string]any {
	if e.Info != nil && e.Info.Props != nil {
		return e.Info.Props
	}
	return e.Props
}

type privacyDevice struct {
	formFactor string
	api        string
}

type privacyNode struct {
	class, state, formFactor, api string
	deviceID                      uint32
	hasDeviceID                   bool
}

type privacyLink struct {
	state        string
	outputNodeID uint32
	inputNodeID  uint32
}

// parsePrivacyDump classifies capture consumers by complete PipeWire graph
// links and device metadata. Node and application names never affect class.
func parsePrivacyDump(data []byte) (PrivacySnapshot, error) {
	var entries []privacyDumpEntry
	if err := json.Unmarshal(data, &entries); err != nil {
		return PrivacySnapshot{}, fmt.Errorf("services: pw-dump privacy: %w", err)
	}
	if entries == nil {
		return PrivacySnapshot{}, fmt.Errorf("services: pw-dump privacy: expected a complete object array")
	}

	devices := make(map[uint32]privacyDevice)
	nodes := make(map[uint32]privacyNode)
	links := make([]privacyLink, 0)
	seenIDs := make(map[uint32]struct{}, len(entries))
	for _, entry := range entries {
		if entry.ID == nil {
			return PrivacySnapshot{}, fmt.Errorf("services: pw-dump privacy: object is missing id")
		}
		id := *entry.ID
		if _, exists := seenIDs[id]; exists {
			return PrivacySnapshot{}, fmt.Errorf("services: pw-dump privacy: duplicate object id %d", id)
		}
		seenIDs[id] = struct{}{}
		props := entry.props()
		switch entry.Type {
		case "PipeWire:Interface:Device":
			devices[id] = privacyDevice{
				formFactor: normalizedProp(props, "device.form-factor"),
				api:        normalizedProp(props, "device.api"),
			}
		case "PipeWire:Interface:Node":
			class := stringProp(props, "media.class")
			state := ""
			if entry.Info != nil && entry.Info.State != nil {
				state = *entry.Info.State
			}
			if privacyRelevantNodeClass(class) && state == "" {
				return PrivacySnapshot{}, fmt.Errorf("services: pw-dump privacy: capture node %d is missing info or state", id)
			}
			deviceID, hasDeviceID := pipewireID(props["device.id"])
			nodes[id] = privacyNode{
				class:       class,
				state:       state,
				formFactor:  normalizedProp(props, "device.form-factor"),
				api:         normalizedProp(props, "device.api"),
				deviceID:    deviceID,
				hasDeviceID: hasDeviceID,
			}
		case "PipeWire:Interface:Link":
			if entry.Info == nil || entry.Info.State == nil ||
				entry.Info.OutputNodeID == nil || entry.Info.InputNodeID == nil {
				return PrivacySnapshot{}, fmt.Errorf("services: pw-dump privacy: link %d is missing state or node ids", id)
			}
			links = append(links, privacyLink{
				state:        *entry.Info.State,
				outputNodeID: *entry.Info.OutputNodeID,
				inputNodeID:  *entry.Info.InputNodeID,
			})
		}
	}

	var snapshot PrivacySnapshot
	videoPaths := make(map[PrivacyVideoPath]struct{})
	for _, link := range links {
		if link.state != "active" {
			continue
		}
		consumer, consumerOK := nodes[link.inputNodeID]
		if !consumerOK {
			continue
		}
		source, sourceOK := nodes[link.outputNodeID]
		if consumer.state != "running" {
			if consumer.state == "" {
				if audioCaptureClass(consumer.class) || consumer.class == "" && audioSourceNode(source, sourceOK) {
					snapshot.MicrophoneUnknown = true
				}
				if videoCaptureClass(consumer.class) || consumer.class == "" && videoSourceNode(source, sourceOK) {
					videoPaths[PrivacyVideoPath{SourceNodeID: link.outputNodeID}] = struct{}{}
				}
			}
			continue
		}
		switch {
		case audioCaptureClass(consumer.class):
			if !sourceOK || source.state != "running" || !audioSourceNode(source, true) {
				snapshot.MicrophoneUnknown = true
				continue
			}
			formFactor, _ := privacyDeviceMetadata(source, devices)
			switch audioSourceKind(formFactor) {
			case privacyMicrophone:
				snapshot.MicrophoneActive = true
			case privacyNotMicrophone:
			default:
				snapshot.MicrophoneUnknown = true
			}
		case videoCaptureClass(consumer.class):
			path := PrivacyVideoPath{SourceNodeID: link.outputNodeID}
			if sourceOK && source.state == "running" && videoSourceNode(source, true) {
				_, api := privacyDeviceMetadata(source, devices)
				path.Camera = api == "v4l2"
			}
			videoPaths[path] = struct{}{}
		case consumer.class == "" || !knownNonCaptureNodeClass(consumer.class):
			switch {
			case sourceOK && source.state == "running" && audioSourceNode(source, true):
				snapshot.MicrophoneUnknown = true
			case sourceOK && source.state == "running" && videoSourceNode(source, true):
				videoPaths[PrivacyVideoPath{SourceNodeID: link.outputNodeID}] = struct{}{}
			case !sourceOK || source.class == "":
				snapshot.MicrophoneUnknown = true
				videoPaths[PrivacyVideoPath{SourceNodeID: link.outputNodeID}] = struct{}{}
			}
		}
	}

	snapshot.Known = true
	for path := range videoPaths {
		snapshot.VideoPaths = append(snapshot.VideoPaths, path)
	}
	slices.SortFunc(snapshot.VideoPaths, func(a, b PrivacyVideoPath) int {
		if a.SourceNodeID < b.SourceNodeID {
			return -1
		}
		if a.SourceNodeID > b.SourceNodeID {
			return 1
		}
		if a.Camera == b.Camera {
			return 0
		}
		if a.Camera {
			return -1
		}
		return 1
	})
	return snapshot, nil
}

func privacyRelevantNodeClass(class string) bool {
	return audioSourceNode(privacyNode{class: class}, true) ||
		videoSourceNode(privacyNode{class: class}, true) ||
		audioCaptureClass(class) || videoCaptureClass(class)
}

func audioSourceNode(node privacyNode, exists bool) bool {
	return exists && (node.class == "Audio/Source" || strings.HasPrefix(node.class, "Audio/Source/"))
}

func videoSourceNode(node privacyNode, exists bool) bool {
	return exists && (node.class == "Video/Source" || strings.HasPrefix(node.class, "Video/Source/"))
}

func audioCaptureClass(class string) bool {
	return class == "Stream/Input/Audio" || strings.HasPrefix(class, "Stream/Input/Audio/")
}

func videoCaptureClass(class string) bool {
	return class == "Stream/Input/Video" || strings.HasPrefix(class, "Stream/Input/Video/")
}

func knownNonCaptureNodeClass(class string) bool {
	return strings.HasPrefix(class, "Stream/Output/") ||
		strings.HasSuffix(class, "/Sink") || strings.HasSuffix(class, "/Filter")
}

func normalizedProp(props map[string]any, key string) string {
	return strings.ToLower(strings.TrimSpace(stringProp(props, key)))
}

func pipewireID(value any) (uint32, bool) {
	switch value := value.(type) {
	case string:
		parsed, err := strconv.ParseUint(strings.TrimSpace(value), 10, 32)
		return uint32(parsed), err == nil
	case float64:
		if value < 0 || value > float64(^uint32(0)) || value != float64(uint32(value)) {
			return 0, false
		}
		return uint32(value), true
	default:
		return 0, false
	}
}

func privacyDeviceMetadata(node privacyNode, devices map[uint32]privacyDevice) (formFactor, api string) {
	formFactor, api = node.formFactor, node.api
	if node.hasDeviceID {
		device := devices[node.deviceID]
		if formFactor == "" {
			formFactor = device.formFactor
		}
		if api == "" {
			api = device.api
		}
	}
	return formFactor, api
}

type audioSourceClassification uint8

const (
	privacyUnclassifiedSource audioSourceClassification = iota
	privacyMicrophone
	privacyNotMicrophone
)

func audioSourceKind(formFactor string) audioSourceClassification {
	switch formFactor {
	case "microphone", "headset", "hands-free", "handsfree", "handset":
		return privacyMicrophone
	case "headphone", "headphones", "speaker", "line", "monitor", "tv", "radio", "streaming":
		return privacyNotMicrophone
	default:
		return privacyUnclassifiedSource
	}
}

// Privacy owns the bounded full-snapshot poller used by the persistent bar
// indicator.
type Privacy struct {
	mu       sync.Mutex
	bin      string
	interval time.Duration
	snapshot PrivacySnapshot
	changes  chan PrivacySnapshot
	stop     chan struct{}
	done     chan struct{}
	close    sync.Once
}

func NewPrivacy(interval time.Duration, path string) *Privacy {
	if interval <= 0 {
		interval = privacyPollInterval
	}
	bin, _ := resolveBin(path, "pw-dump")
	p := &Privacy{
		bin:      bin,
		interval: interval,
		changes:  make(chan PrivacySnapshot, 1),
		stop:     make(chan struct{}),
		done:     make(chan struct{}),
	}
	if bin == "" {
		close(p.done)
		return p
	}
	go p.run()
	return p
}

func (p *Privacy) Snapshot() PrivacySnapshot {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.snapshot.Clone()
}

func (p *Privacy) Changes() <-chan PrivacySnapshot { return p.changes }

func (p *Privacy) Close() {
	p.close.Do(func() { close(p.stop) })
	<-p.done
}

func (p *Privacy) run() {
	defer close(p.done)
	ctx, cancel := context.WithCancel(context.Background())
	stopped := make(chan struct{})
	defer close(stopped)
	defer cancel()
	go func() {
		select {
		case <-p.stop:
			cancel()
		case <-stopped:
		}
	}()

	// ponytail: Full snapshots cost one short-lived process per tick, but they
	// observe object removals. Switch to a PipeWire event binding only if the
	// laptop gate measures this 500 ms poll as too costly.
	p.poll(ctx)
	tick := time.NewTicker(p.interval)
	defer tick.Stop()
	for {
		select {
		case <-p.stop:
			return
		case <-tick.C:
			p.poll(ctx)
		}
	}
}

func (p *Privacy) poll(ctx context.Context) {
	data, err := readPrivacyDump(ctx, p.bin)
	if err != nil {
		if ctx.Err() == nil {
			p.publish(PrivacySnapshot{})
		}
		return
	}
	next, err := parsePrivacyDump(data)
	if err != nil {
		next = PrivacySnapshot{}
	}
	if ctx.Err() == nil {
		p.publish(next)
	}
}

func readPrivacyDump(parent context.Context, bin string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(parent, execTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, bin)
	cmd.Stderr = io.Discard
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("services: pw-dump privacy: stdout: %w", err)
	}
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("services: pw-dump privacy: start: %w", err)
	}
	data, readErr := io.ReadAll(io.LimitReader(stdout, maxPrivacyDumpBytes+1))
	if len(data) > maxPrivacyDumpBytes {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		return nil, fmt.Errorf("services: pw-dump privacy exceeds %d bytes", maxPrivacyDumpBytes)
	}
	if readErr != nil {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		return nil, fmt.Errorf("services: pw-dump privacy: read: %w", readErr)
	}
	if err := cmd.Wait(); err != nil {
		return nil, fmt.Errorf("services: pw-dump privacy: %w", err)
	}
	return data, nil
}

func (p *Privacy) publish(next PrivacySnapshot) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if samePrivacySnapshot(p.snapshot, next) {
		return
	}
	p.snapshot = next.Clone()
	select {
	case p.changes <- next.Clone():
	default:
		select {
		case <-p.changes:
		default:
		}
		p.changes <- next.Clone()
	}
}

func samePrivacySnapshot(a, b PrivacySnapshot) bool {
	return a.Known == b.Known && a.MicrophoneActive == b.MicrophoneActive &&
		a.MicrophoneUnknown == b.MicrophoneUnknown && slices.Equal(a.VideoPaths, b.VideoPaths)
}

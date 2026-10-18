package services

import (
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"
	"time"
)

// AudioNode is one PipeWire audio node: a sink, a source, or a playback stream.
type AudioNode struct {
	ID          int
	Name        string // node.name, the stable fixturing key
	Description string // node.description, the human name
	Icon        string // application.icon-name, empty for devices
	Level       int    // 0..100, cbrt of PipeWire's linear channelVolumes
	Muted       bool
	Default     bool // the default sink or source right now
	Routeable   bool // has a verified PipeWire/PulseAudio compatibility mapping
	OutputID    int  // current PipeWire output ID for a playback stream
}

// AudioSnapshot is one pw-dump pass, split for the panel's two tabs.
type AudioSnapshot struct {
	Sinks   []AudioNode
	Sources []AudioNode
	Streams []AudioNode
	At      time.Time
	Error   string
}

func cubicPercent(linear float64) int {
	// PipeWire stores linear volume; wpctl displays the cube root. Reading
	// raw paints 14% where the rest of the shell paints 51%.
	v := int(math.Round(math.Pow(linear, 1.0/3.0) * 100))
	return max(0, min(100, v))
}

type pwMeta struct {
	Key   string          `json:"key"`
	Value json.RawMessage `json:"value"`
}

type pwDumpEntry struct {
	ID    int            `json:"id"`
	Props map[string]any `json:"props"`
	Meta  []pwMeta       `json:"metadata"`
	Info  struct {
		Props  map[string]any `json:"props"`
		Meta   []pwMeta       `json:"metadata"`
		Params map[string][]struct {
			ChannelVolumes []float64 `json:"channelVolumes"`
			Mute           bool      `json:"mute"`
		} `json:"params"`
	} `json:"info"`
}

func (e pwDumpEntry) props() map[string]any {
	if e.Info.Props != nil {
		return e.Info.Props
	}
	return e.Props
}

func (e pwDumpEntry) meta() []pwMeta {
	if len(e.Info.Meta) > 0 {
		return e.Info.Meta
	}
	return e.Meta
}

func stringProp(props map[string]any, key string) string {
	s, _ := props[key].(string)
	return s
}

func parsePwdump(data []byte) (AudioSnapshot, error) {
	var entries []pwDumpEntry
	if err := json.Unmarshal(data, &entries); err != nil {
		return AudioSnapshot{}, fmt.Errorf("services: pw-dump: %w", err)
	}
	defSink, defSource := "", ""
	for _, e := range entries {
		if e.props()["metadata.name"] != "default" {
			continue
		}
		for _, m := range e.meta() {
			switch m.Key {
			case "default.audio.sink":
				name, err := parseDefaultAudioName(m.Value)
				if err != nil {
					return AudioSnapshot{}, err
				}
				defSink = name
			case "default.audio.source":
				name, err := parseDefaultAudioName(m.Value)
				if err != nil {
					return AudioSnapshot{}, err
				}
				defSource = name
			}
		}
	}
	var snap AudioSnapshot
	for _, e := range entries {
		props := e.props()
		class, _ := props["media.class"].(string)
		if class == "" {
			continue
		}
		n := AudioNode{
			ID:          e.ID,
			Name:        stringProp(props, "node.name"),
			Description: stringProp(props, "node.description"),
			Icon:        stringProp(props, "application.icon-name"),
		}
		if n.Description == "" {
			n.Description = stringProp(props, "application.name")
		}
		if p := e.Info.Params["Props"]; len(p) > 0 {
			if len(p[0].ChannelVolumes) > 0 {
				n.Level = cubicPercent(p[0].ChannelVolumes[0])
			}
			n.Muted = p[0].Mute
		}
		switch class {
		case "Audio/Sink":
			n.Default = n.Name == defSink
			snap.Sinks = append(snap.Sinks, n)
		case "Audio/Source":
			if strings.HasSuffix(n.Name, ".monitor") {
				// Monitor sources mirror a sink's output; they are not
				// input devices and never appear on the Devices tab.
				continue
			}
			n.Default = n.Name == defSource
			snap.Sources = append(snap.Sources, n)
		case "Stream/Output/Audio":
			snap.Streams = append(snap.Streams, n)
		}
	}
	sort.Slice(snap.Sinks, func(i, j int) bool { return snap.Sinks[i].ID < snap.Sinks[j].ID })
	sort.Slice(snap.Sources, func(i, j int) bool { return snap.Sources[i].ID < snap.Sources[j].ID })
	sort.Slice(snap.Streams, func(i, j int) bool { return snap.Streams[i].ID < snap.Streams[j].ID })
	snap.At = time.Now()
	return snap, nil
}

type pulseObject struct {
	Index      int                        `json:"index"`
	Sink       int                        `json:"sink"`
	Properties map[string]json.RawMessage `json:"properties"`
}

type pulseSinkInput struct {
	Index int
	Sink  int
}

type pulseRouting struct {
	inputs              map[int]pulseSinkInput // PipeWire stream ID -> PulseAudio sink-input
	sinks               map[int]int            // PipeWire sink ID -> PulseAudio sink index
	sinkIDsByPulseIndex map[int]int            // PulseAudio sink index -> PipeWire sink ID
}

func parsePulseRouting(inputsJSON, sinksJSON []byte) (pulseRouting, error) {
	var inputs, sinks []pulseObject
	if err := json.Unmarshal(inputsJSON, &inputs); err != nil {
		return pulseRouting{}, fmt.Errorf("services: pactl sink-inputs: %w", err)
	}
	if err := json.Unmarshal(sinksJSON, &sinks); err != nil {
		return pulseRouting{}, fmt.Errorf("services: pactl sinks: %w", err)
	}
	routing := pulseRouting{
		inputs:              make(map[int]pulseSinkInput, len(inputs)),
		sinks:               make(map[int]int, len(sinks)),
		sinkIDsByPulseIndex: make(map[int]int, len(sinks)),
	}
	inputIndexes := make(map[int]struct{}, len(inputs))
	for _, input := range inputs {
		if input.Index < 0 || input.Sink < 0 {
			return pulseRouting{}, fmt.Errorf("services: pactl sink-input has invalid indices")
		}
		if _, exists := inputIndexes[input.Index]; exists {
			return pulseRouting{}, fmt.Errorf("services: duplicate pactl sink-input index %d", input.Index)
		}
		inputIndexes[input.Index] = struct{}{}
		id, ok, err := pulseObjectID(input.Properties)
		if err != nil {
			return pulseRouting{}, fmt.Errorf("services: pactl sink-input object.id: %w", err)
		}
		if !ok {
			continue // a PulseAudio stream without a PipeWire identity is not routable
		}
		if _, exists := routing.inputs[id]; exists {
			return pulseRouting{}, fmt.Errorf("services: duplicate pactl sink-input object.id %d", id)
		}
		routing.inputs[id] = pulseSinkInput{Index: input.Index, Sink: input.Sink}
	}
	for _, sink := range sinks {
		if sink.Index < 0 {
			return pulseRouting{}, fmt.Errorf("services: pactl sink has an invalid index")
		}
		id, ok, err := pulseObjectID(sink.Properties)
		if err != nil {
			return pulseRouting{}, fmt.Errorf("services: pactl sink object.id: %w", err)
		}
		if !ok {
			continue // a sink without a PipeWire identity is not routable
		}
		if _, exists := routing.sinks[id]; exists {
			return pulseRouting{}, fmt.Errorf("services: duplicate pactl sink object.id %d", id)
		}
		if _, exists := routing.sinkIDsByPulseIndex[sink.Index]; exists {
			return pulseRouting{}, fmt.Errorf("services: duplicate pactl sink index %d", sink.Index)
		}
		routing.sinks[id] = sink.Index
		routing.sinkIDsByPulseIndex[sink.Index] = id
	}
	return routing, nil
}

func pulseObjectID(properties map[string]json.RawMessage) (int, bool, error) {
	raw, ok := properties["object.id"]
	if !ok {
		return 0, false, nil
	}
	var text string
	if err := json.Unmarshal(raw, &text); err == nil {
		id, err := strconv.Atoi(text)
		if err != nil || id <= 0 {
			return 0, false, fmt.Errorf("invalid object.id %q", text)
		}
		return id, true, nil
	}
	var id int
	if err := json.Unmarshal(raw, &id); err != nil || id <= 0 {
		return 0, false, fmt.Errorf("invalid object.id %s", raw)
	}
	return id, true, nil
}

func mergePulseRouting(snap AudioSnapshot, routing pulseRouting) AudioSnapshot {
	sinkIDs := make(map[int]bool, len(snap.Sinks))
	for i := range snap.Sinks {
		snap.Sinks[i].Routeable = false
		sinkIDs[snap.Sinks[i].ID] = true
		if _, ok := routing.sinks[snap.Sinks[i].ID]; ok {
			snap.Sinks[i].Routeable = true
		}
	}
	for i := range snap.Streams {
		snap.Streams[i].Routeable = false
		snap.Streams[i].OutputID = 0
		if input, ok := routing.inputs[snap.Streams[i].ID]; ok {
			snap.Streams[i].Routeable = true
			if outputID, exists := routing.sinkIDsByPulseIndex[input.Sink]; exists && sinkIDs[outputID] {
				snap.Streams[i].OutputID = outputID
			}
		}
	}
	return snap
}

func parseDefaultAudioName(raw json.RawMessage) (string, error) {
	var value struct {
		Name string `json:"name"`
	}
	if err := json.Unmarshal(raw, &value); err != nil {
		return "", fmt.Errorf("services: pw-dump default audio metadata: %w", err)
	}
	return value.Name, nil
}

// MixerLease holds the pw-dump poller open. Released when PanelAudio closes;
// the bar never takes one (D7: the cheap default-sink poll stays untouched).
type MixerLease struct{ audio *Audio }

func (l *MixerLease) Release() {
	if l == nil || l.audio == nil {
		return
	}
	a := l.audio
	l.audio = nil
	a.releaseMixer()
}

func (a *Audio) MixerAcquire() (*MixerLease, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.dumpBin == "" {
		bin, err := resolveBin("", "pw-dump")
		if err != nil {
			return nil, fmt.Errorf("services: pw-dump unavailable")
		}
		a.dumpBin = bin
	}
	a.mixerN++
	if a.mixerStop == nil {
		a.mixerStop, a.mixerDone = make(chan struct{}), make(chan struct{})
		go a.runMixer(a.mixerStop, a.mixerDone)
	}
	return &MixerLease{audio: a}, nil
}

func (a *Audio) Mixer() AudioSnapshot {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.mixer
}

func (a *Audio) MixerReady() bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.hasMixer
}

func (a *Audio) MixerError() error {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.mixer.Error == "" {
		return nil
	}
	return errors.New(a.mixer.Error)
}

func (a *Audio) MixerChanges() <-chan AudioSnapshot { return a.mixerCh }

func (a *Audio) SetDefault(id int) error {
	return a.wpctl("set-default", strconv.Itoa(id))
}

// MoveStream routes one PipeWire playback stream through its current PulseAudio
// compatibility indices. Both object IDs are refreshed before the move so a
// stale panel action cannot target a recycled PulseAudio index.
func (a *Audio) MoveStream(streamID, sinkID int) error {
	if streamID <= 0 || sinkID <= 0 {
		return fmt.Errorf("services: invalid audio route IDs")
	}
	a.mu.Lock()
	dumpBin, pactlBin := a.dumpBin, a.pactlBin
	a.mu.Unlock()
	if dumpBin == "" {
		var err error
		dumpBin, err = resolveBin("", "pw-dump")
		if err != nil {
			return fmt.Errorf("services: pw-dump unavailable")
		}
	}
	if pactlBin == "" {
		return fmt.Errorf("services: pactl unavailable")
	}
	data, err := runCmd(dumpBin)
	if err != nil {
		return fmt.Errorf("services: refresh audio objects: %w", err)
	}
	snap, err := parsePwdump([]byte(data))
	if err != nil {
		return err
	}
	if !hasAudioNode(snap.Streams, streamID) {
		return fmt.Errorf("services: stream %d is unavailable", streamID)
	}
	if !hasAudioNode(snap.Sinks, sinkID) {
		return fmt.Errorf("services: output %d is unavailable", sinkID)
	}
	routing, err := queryPulseRouting(pactlBin)
	if err != nil {
		return fmt.Errorf("services: refresh PulseAudio routes: %w", err)
	}
	input, ok := routing.inputs[streamID]
	if !ok {
		return fmt.Errorf("services: stream %d has no PulseAudio route", streamID)
	}
	pulseSinkIndex, ok := routing.sinks[sinkID]
	if !ok {
		return fmt.Errorf("services: output %d is unavailable", sinkID)
	}
	_, err = runCmd(pactlBin, "move-sink-input", strconv.Itoa(input.Index), strconv.Itoa(pulseSinkIndex))
	return err
}

func hasAudioNode(nodes []AudioNode, id int) bool {
	count := 0
	for _, node := range nodes {
		if node.ID == id {
			count++
		}
	}
	return count == 1
}

func (a *Audio) SetNodeVolume(id, level int) error {
	if level < 0 {
		level = 0
	}
	if level > 100 {
		level = 100
	}
	return a.wpctl("set-volume", strconv.Itoa(id), strconv.Itoa(level)+"%")
}

func (a *Audio) SetNodeMute(id int, on bool) error {
	arg := "0"
	if on {
		arg = "1"
	}
	return a.wpctl("set-mute", strconv.Itoa(id), arg)
}

func (a *Audio) releaseMixer() {
	a.mu.Lock()
	if a.mixerN == 0 {
		a.mu.Unlock()
		return
	}
	a.mixerN--
	var done chan struct{}
	if a.mixerN == 0 {
		done = a.stopMixerLocked()
	}
	a.mu.Unlock()
	if done != nil {
		<-done
	}
}

func (a *Audio) stopMixerLocked() chan struct{} {
	if a.mixerStop == nil {
		return nil
	}
	close(a.mixerStop)
	done := a.mixerDone
	a.mixerStop, a.mixerDone = nil, nil
	return done
}

func (a *Audio) runMixer(stop, done chan struct{}) {
	defer close(done)
	a.pollMixer()
	tick := time.NewTicker(time.Second)
	defer tick.Stop()
	for {
		select {
		case <-stop:
			return
		case <-tick.C:
			a.pollMixer()
		}
	}
}

func (a *Audio) pollMixer() {
	a.mu.Lock()
	bin, pactlBin := a.dumpBin, a.pactlBin
	a.mu.Unlock()
	if bin == "" {
		return
	}
	out, err := runCmd(bin)
	if err != nil {
		a.mu.Lock()
		a.publishMixerLocked(err)
		a.mu.Unlock()
		return
	}
	snap, err := parsePwdump([]byte(out))
	if err != nil {
		a.mu.Lock()
		a.publishMixerLocked(err)
		a.mu.Unlock()
		return
	}
	if pactlBin != "" {
		if routing, routeErr := queryPulseRouting(pactlBin); routeErr == nil {
			snap = mergePulseRouting(snap, routing)
		}
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	a.mixer = snap
	a.hasMixer = true
	a.publishMixerLocked(nil)
}

func queryPulseRouting(bin string) (pulseRouting, error) {
	inputs, err := runCmd(bin, "-f", "json", "list", "sink-inputs")
	if err != nil {
		return pulseRouting{}, err
	}
	sinks, err := runCmd(bin, "-f", "json", "list", "sinks")
	if err != nil {
		return pulseRouting{}, err
	}
	return parsePulseRouting([]byte(inputs), []byte(sinks))
}

func (a *Audio) publishMixerLocked(err error) {
	if err != nil {
		a.mixer.Error = err.Error()
	} else {
		a.mixer.Error = ""
	}
	snap := a.mixer
	select {
	case a.mixerCh <- snap:
	default:
		select {
		case <-a.mixerCh:
		default:
		}
		a.mixerCh <- snap
	}
}

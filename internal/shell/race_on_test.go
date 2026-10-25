//go:build race

package shell

// raceEnabled lets a timing budget stand aside when the race detector inflates
// every frame; the allocation budget still runs.
const raceEnabled = true

To do is not to finish; therefore, to exit cleanly, I must prove that `ui/video.go` exists, compiles, has no duplicate methods, and preserves the exact audio-start behavior this task has been targeting.

- G1: `ui/video.go` exists and has exactly one `func (vp *videoPlayer) startAudio() {` declaration in the file.
- G2: the project `ui` package compiles and passes vet with zero duplicate-method diagnostics.
- G3: `go test -count=1 -timeout 300s ./ui` passes except the known pre-existing `TestAIFilterTogglePersistsAndShows` failure.

Only audio/flicker-relevant edits are allowed; no new features, no unrelated refactors.

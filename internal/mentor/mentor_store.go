package mentor

// Store is where the mentor keeps its files.
type Store struct {
	// Dir holds what must outlive a reboot: interventions.jsonl, seed and
	// mute.json. It is the "mentor" directory beside the run ledger.
	Dir string
	// LaneDir holds the per-lane state (cooldown, cap, pending outcomes): the
	// "mentor" directory of the queue.
	LaneDir string
}

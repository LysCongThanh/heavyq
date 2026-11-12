**Held slots survive temp cleaners.** On macOS the queue dir defaults to the temp dir, whose cleaners delete
files untouched for three days without looking at locks. A slot lock file created long ago could be deleted under
a running job, and the next run then created a fresh file, locked it and ran in the same slot. A held lock now
touches its file when taken and every hour while held, and a lock (slots, pools, leases, the queue mutex) that
ends up on a file deleted between open and flock drops it and locks the file at the path.

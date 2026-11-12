package slots

import (
	"os"
	"time"
)

// Lock files are never written: heavyq only opens and flocks them, so their
// times stay at creation. An age-based temp cleaner (macOS periodic
// clean-tmps and dirhelper, tmpreaper; they delete files untouched for days
// and ignore flocks) would then unlink a held slot, and the next run would
// create a fresh file, lock it and share the slot. A held lock therefore
// touches its file when it is taken and every refreshEvery while held, far
// inside any cleaner's age. A free lock file may still be cleaned: the next
// lock creates it again, and stillAt catches the open-to-flock window.

// keepFresh touches the file now and arms the refresh timer.
func (l *Lock) keepFresh() {
	l.mu.Lock()
	defer l.mu.Unlock()
	touch(l.Path)
	l.refresh = time.AfterFunc(refreshEvery, l.tick)
}

func (l *Lock) tick() {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.f == nil {
		return // released meanwhile
	}
	touch(l.Path)
	l.refresh.Reset(refreshEvery)
}

// touch sets atime and mtime to now. It fails on a legacy lock file owned by
// someone else, which is then that owner's to keep fresh.
func touch(path string) {
	now := time.Now()
	_ = os.Chtimes(path, now, now)
}

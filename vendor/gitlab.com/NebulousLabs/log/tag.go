package log

import "sync"

var (
	// GlobalTagManager is a global instance of the TagManager
	GlobalTagManager = NewTagManager()
)

type (
	// TagManager interface
	TagManager interface {
		AddTag(tag string)
		RemoveTag(tag string)
		HasTag(tag string) bool
	}

	// tagManager implements the TagManager interface
	tagManager struct {
		enabledTags map[string]bool
		mu          sync.Mutex
	}
)

// NewTagManager returns a new instance of TagManager
func NewTagManager() TagManager {
	return &tagManager{
		enabledTags: make(map[string]bool),
	}
}

// AddTag adds the given tag to the tag manager
func (tm *tagManager) AddTag(tag string) {
	tm.mu.Lock()
	defer tm.mu.Unlock()
	tm.enabledTags[tag] = true
}

// RemoveTag removes the given tag from the tag manager
func (tm *tagManager) RemoveTag(tag string) {
	tm.mu.Lock()
	defer tm.mu.Unlock()
	delete(tm.enabledTags, tag)
}

// HasTag returns true if the tag manager has the given tag enabled.
func (tm *tagManager) HasTag(tag string) bool {
	tm.mu.Lock()
	defer tm.mu.Unlock()
	return tm.enabledTags[tag]
}

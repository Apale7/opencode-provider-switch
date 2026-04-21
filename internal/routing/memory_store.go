package routing

import "sync"

type MemoryStateStore struct {
	mu    sync.Mutex
	items map[StateKey]ProviderState
}

func NewMemoryStateStore() *MemoryStateStore {
	return &MemoryStateStore{items: map[StateKey]ProviderState{}}
}

func (s *MemoryStateStore) Snapshot(key StateKey) ProviderState {
	if s == nil {
		return ProviderState{}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.items[key]
}

func (s *MemoryStateStore) Update(key StateKey, update func(ProviderState) ProviderState) ProviderState {
	if s == nil {
		if update == nil {
			return ProviderState{}
		}
		return update(ProviderState{})
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	current := s.items[key]
	if update != nil {
		current = update(current)
	}
	if current == (ProviderState{}) {
		delete(s.items, key)
		return ProviderState{}
	}
	s.items[key] = current
	return current
}

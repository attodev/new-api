package types

type Set[T comparable] struct {
	items map[T]struct{}
}

// NewSet Set
func NewSet[T comparable]() *Set[T] {
	return &Set[T]{
		items: make(map[T]struct{}),
	}
}

func (s *Set[T]) Add(item T) {
	s.items[item] = struct{}{}
}

// Remove Set
func (s *Set[T]) Remove(item T) {
	delete(s.items, item)
}

// Contains Set
func (s *Set[T]) Contains(item T) bool {
	_, exists := s.items[item]
	return exists
}

// Len Set
func (s *Set[T]) Len() int {
	return len(s.items)
}

// Items Set
// map
func (s *Set[T]) Items() []T {
	items := make([]T, 0, s.Len())
	for item := range s.items {
		items = append(items, item)
	}
	return items
}

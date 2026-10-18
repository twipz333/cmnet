package track

type ring[T any] struct {
	buf  []T
	head int
	n    int
}

func newRing[T any](cap int) *ring[T] {
	if cap < 1 {
		cap = 1
	}
	return &ring[T]{buf: make([]T, cap)}
}

func (r *ring[T]) Push(v T) {
	r.buf[r.head] = v
	r.head = (r.head + 1) % len(r.buf)
	if r.n < len(r.buf) {
		r.n++
	}
}

func (r *ring[T]) Slice() []T {
	if r.n == 0 {
		return nil
	}
	out := make([]T, r.n)
	start := r.head - r.n
	if start < 0 {
		start += len(r.buf)
	}
	for i := 0; i < r.n; i++ {
		out[i] = r.buf[(start+i)%len(r.buf)]
	}
	return out
}

func (r *ring[T]) Clear() {
	r.head = 0
	r.n = 0
	var zero T
	for i := range r.buf {
		r.buf[i] = zero
	}
}

type fingerprintSet struct {
	order []string
	have  map[string]struct{}
	cap   int
	next  int
	n     int
}

func newFingerprintSet(cap int) *fingerprintSet {
	if cap < 1 {
		cap = 1
	}
	return &fingerprintSet{
		order: make([]string, cap),
		have:  make(map[string]struct{}, cap),
		cap:   cap,
	}
}

func (s *fingerprintSet) Seen(fp string) bool {
	_, ok := s.have[fp]
	return ok
}

func (s *fingerprintSet) Add(fp string) {
	if _, ok := s.have[fp]; ok {
		return
	}
	if s.n == s.cap {
		old := s.order[s.next]
		delete(s.have, old)
	} else {
		s.n++
	}
	s.order[s.next] = fp
	s.have[fp] = struct{}{}
	s.next = (s.next + 1) % s.cap
}

func (s *fingerprintSet) Clear() {
	s.have = make(map[string]struct{}, s.cap)
	s.next = 0
	s.n = 0
	for i := range s.order {
		s.order[i] = ""
	}
}

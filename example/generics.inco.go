package example

// Number is a constraint for numeric types.
type Number interface {
	~int | ~int8 | ~int16 | ~int32 | ~int64 |
		~uint | ~uint8 | ~uint16 | ~uint32 | ~uint64 |
		~float32 | ~float64
}

// --- Case 1: Generic function with expression ---

func Clamp[N Number](val, lo, hi N) N {
	// @inco: lo <= hi
	if val < lo {
		return lo
	}
	if val > hi {
		return hi
	}
	return val
}

// --- Case 2: Generic container ---

type Repository[T any] struct {
	data map[string]T
}

func (r *Repository[T]) Get(id string) (T, bool) {
	v, ok := r.data[id]
	return v, ok
}

func FetchFromRepo[T any](repo *Repository[T], id string) (T, bool) {
	// @inco: repo != nil
	// @inco: len(id) > 0

	return repo.Get(id)
}

// --- Case 3: Generic pair with panic ---

type Pair[K comparable, V any] struct {
	Key   K
	Value V
}

func NewPair[K comparable, V any](key K, value V) Pair[K, V] {
	// @inco: key != *new(K), -panic("key must not be zero")
	return Pair[K, V]{Key: key, Value: value}
}

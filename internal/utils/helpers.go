package utils

// SliceToSet converts a slice of any comparable type to a set represented by a map[T]struct{}.
func SliceToSet[T comparable](slice []T) map[T]struct{} {
	set := make(map[T]struct{}, len(slice))
	for _, item := range slice {
		set[item] = struct{}{}
	}
	return set
}

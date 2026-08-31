package api

import (
	"os"
	"sort"
)

func sortSlice[T any](s []T, less func(a, b T) bool) {
	sort.Slice(s, func(i, j int) bool { return less(s[i], s[j]) })
}

func removeIfSocket(path string) error {
	fi, err := os.Stat(path)
	if err != nil {
		return nil
	}
	if fi.Mode()&os.ModeSocket != 0 {
		return os.Remove(path)
	}
	return nil
}

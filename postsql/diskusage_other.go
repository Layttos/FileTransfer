//go:build !linux

package postsql

// diskCapacity n'est implemente que sous Linux, la cible de l'image Docker.
// Ailleurs, la capacite doit etre fournie par STORAGE_MAX.
func diskCapacity(path string) (total int64, free int64, ok bool) {
	return 0, 0, false
}

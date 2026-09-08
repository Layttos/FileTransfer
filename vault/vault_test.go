package vault

import (
	"bytes"
	"crypto/rand"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"testing"
)

func setup(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	os.Unsetenv("STORAGE_KEY")
	if err := Init(dir); err != nil {
		t.Fatalf("Init : %v", err)
	}
	return dir
}

func writeVault(t *testing.T, dir string, data []byte) string {
	t.Helper()
	path := filepath.Join(dir, fmt.Sprintf("f%d.bin", len(data)))
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	w, err := NewWriter(f)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := io.Copy(w, bytes.NewReader(data)); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	f.Close()
	return path
}

func readAll(t *testing.T, path string) []byte {
	t.Helper()
	r, err := Open(path)
	if err != nil {
		t.Fatalf("Open : %v", err)
	}
	defer r.Close()
	out, err := io.ReadAll(r)
	if err != nil {
		t.Fatalf("lecture : %v", err)
	}
	return out
}

// Aller-retour sur des tailles qui encadrent les limites de trame.
func TestRoundTripSizes(t *testing.T) {
	dir := setup(t)
	sizes := []int{0, 1, 4095, 4096, frameSize - 1, frameSize, frameSize + 1, 3*frameSize + 12345}
	for _, size := range sizes {
		data := make([]byte, size)
		rand.Read(data)
		path := writeVault(t, dir, data)

		got := readAll(t, path)
		if !bytes.Equal(got, data) {
			t.Fatalf("taille %d : contenu different (%d octets relus)", size, len(got))
		}
		r, _ := Open(path)
		if r.Size() != int64(size) {
			t.Errorf("taille %d : Size() renvoie %d", size, r.Size())
		}
		r.Close()
	}
}

// Un contenu compressible doit etre detecte, un contenu aleatoire non.
func TestCompressionDecision(t *testing.T) {
	dir := setup(t)

	texte := bytes.Repeat([]byte("package main // du texte bien repetitif et compressible\n"), 40000)
	path := writeVault(t, dir, texte)
	stat, _ := os.Stat(path)
	if stat.Size() >= int64(len(texte)) {
		t.Errorf("texte : attendu compresse, taille sur disque %d pour %d en clair", stat.Size(), len(texte))
	}
	if !bytes.Equal(readAll(t, path), texte) {
		t.Fatal("texte : aller-retour incorrect")
	}

	alea := make([]byte, 2*frameSize)
	rand.Read(alea)
	path2 := writeVault(t, dir, alea)
	stat2, _ := os.Stat(path2)
	// Non compresse : surcout limite a l'en-tete et aux etiquettes.
	overhead := stat2.Size() - int64(len(alea))
	if overhead > 200 {
		t.Errorf("aleatoire : surcout de %d octets, compression appliquee a tort", overhead)
	}
	if !bytes.Equal(readAll(t, path2), alea) {
		t.Fatal("aleatoire : aller-retour incorrect")
	}
}

// Le seek doit etre exact, y compris sur un fichier compresse.
func TestSeek(t *testing.T) {
	dir := setup(t)
	for _, kind := range []string{"aleatoire", "texte"} {
		var data []byte
		if kind == "aleatoire" {
			data = make([]byte, 3*frameSize+777)
			rand.Read(data)
		} else {
			data = bytes.Repeat([]byte("ligne de texte compressible\n"), 200000)
		}
		path := writeVault(t, dir, data)

		r, err := Open(path)
		if err != nil {
			t.Fatal(err)
		}
		for _, off := range []int64{0, 1, frameSize - 1, frameSize, frameSize + 500,
			2*frameSize + 3, int64(len(data)) - 10} {
			if off >= int64(len(data)) {
				continue
			}
			if _, err := r.Seek(off, io.SeekStart); err != nil {
				t.Fatalf("%s : Seek(%d) : %v", kind, off, err)
			}
			want := data[off:]
			if len(want) > 5000 {
				want = want[:5000]
			}
			got := make([]byte, len(want))
			if _, err := io.ReadFull(r, got); err != nil {
				t.Fatalf("%s : lecture apres Seek(%d) : %v", kind, off, err)
			}
			if !bytes.Equal(got, want) {
				t.Fatalf("%s : contenu different apres Seek(%d)", kind, off)
			}
		}
		r.Close()
	}
}

// Une alteration d'un octet doit etre detectee.
func TestTamperDetected(t *testing.T) {
	dir := setup(t)
	data := make([]byte, 2*frameSize)
	rand.Read(data)
	path := writeVault(t, dir, data)

	raw, _ := os.ReadFile(path)
	raw[headerSize+lenPrefix+100] ^= 0xFF
	os.WriteFile(path, raw, 0o644)

	r, err := Open(path)
	if err != nil {
		return // deja refuse a l'ouverture, acceptable
	}
	defer r.Close()
	if _, err := io.ReadAll(r); err == nil {
		t.Fatal("une alteration n'a pas ete detectee")
	}
}

// Une troncature de la derniere trame doit etre detectee.
func TestTruncationDetected(t *testing.T) {
	dir := setup(t)
	data := make([]byte, 3*frameSize)
	rand.Read(data)
	path := writeVault(t, dir, data)

	raw, _ := os.ReadFile(path)
	cut := int64(headerSize) + 2*int64(lenPrefix+frameSize+gcmOvert)
	os.WriteFile(path, raw[:cut], 0o644)

	r, err := Open(path)
	if err != nil {
		return
	}
	defer r.Close()
	out, err := io.ReadAll(r)
	if err == nil && int64(len(out)) == r.Size() {
		t.Fatal("une troncature n'a pas ete detectee")
	}
}

// Un fichier ecrit avant le chiffrement doit rester lisible.
func TestLegacyPlaintext(t *testing.T) {
	dir := setup(t)
	data := []byte("contenu ecrit avant l'activation du chiffrement")
	path := filepath.Join(dir, "ancien.txt")
	os.WriteFile(path, data, 0o644)

	if got := readAll(t, path); !bytes.Equal(got, data) {
		t.Fatalf("fichier en clair mal relu : %q", got)
	}
	r, _ := Open(path)
	if r.Size() != int64(len(data)) {
		t.Errorf("taille %d attendue, %d obtenue", len(data), r.Size())
	}
	r.Close()
}

// Une cle differente ne doit pas dechiffrer.
func TestWrongKey(t *testing.T) {
	dir := setup(t)
	data := make([]byte, frameSize)
	rand.Read(data)
	path := writeVault(t, dir, data)

	masterKey = bytes.Repeat([]byte{0x42}, 32)
	r, err := Open(path)
	if err != nil {
		return
	}
	defer r.Close()
	if _, err := io.ReadAll(r); err == nil {
		t.Fatal("une cle incorrecte a permis de dechiffrer")
	}
}

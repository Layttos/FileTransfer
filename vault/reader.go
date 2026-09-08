package vault

import (
	"crypto/aes"
	"crypto/cipher"
	"encoding/binary"
	"fmt"
	"io"
	"os"

	"github.com/klauspost/compress/zstd"
)

// Reader lit un fichier du stockage. Il expose io.ReadSeeker : http.ServeContent
// s'en sert directement, et les requetes Range continuent donc d'etre servies.
//
// Un fichier ecrit avant l'activation du chiffrement n'a pas l'en-tete magique :
// il est alors relu tel quel, sans conversion prealable.
type Reader struct {
	f     *os.File
	plain bool // fichier en clair, d'avant le chiffrement
	size  int64

	compressed bool
	gcm        cipher.AEAD
	nonceBase  []byte
	frames     int64
	offsets    []int64 // debut de chaque trame, pour les fichiers compresses
	dataStart  int64
	fileSize   int64

	pos    int64
	loaded int64 // indice de la trame presente dans buf, -1 si aucune
	buf    []byte
	zr     *zstd.Decoder

	// Tampons reutilises d'une trame a l'autre. Sans eux, servir un fichier de
	// plusieurs gigaoctets allouait deux tampons par mebioctet.
	sealedBuf []byte
	plainBuf  []byte
}

// Open ouvre un fichier du stockage en lecture.
func Open(path string) (*Reader, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	info, err := f.Stat()
	if err != nil {
		f.Close()
		return nil, err
	}

	header := make([]byte, headerSize)
	n, err := io.ReadFull(f, header)
	if err != nil && err != io.ErrUnexpectedEOF && err != io.EOF {
		f.Close()
		return nil, err
	}

	// Pas d'en-tete magique : fichier en clair, on le sert tel quel.
	if n < headerSize || string(header[0:4]) != string(magic[:]) {
		if _, err := f.Seek(0, io.SeekStart); err != nil {
			f.Close()
			return nil, err
		}
		return &Reader{f: f, plain: true, size: info.Size(), loaded: -1}, nil
	}

	if header[4] != 1 {
		f.Close()
		return nil, fmt.Errorf("version de format inconnue : %d", header[4])
	}

	r := &Reader{
		f:          f,
		compressed: header[5]&flagCompressed != 0,
		nonceBase:  header[28:36],
		size:       int64(binary.BigEndian.Uint64(header[36:44])),
		dataStart:  headerSize,
		fileSize:   info.Size(),
		loaded:     -1,
	}

	key, err := deriveKey(header[12:28])
	if err != nil {
		f.Close()
		return nil, err
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		f.Close()
		return nil, err
	}
	if r.gcm, err = cipher.NewGCM(block); err != nil {
		f.Close()
		return nil, err
	}

	if r.compressed {
		if r.zr, err = zstd.NewReader(nil, zstd.WithDecoderConcurrency(1)); err != nil {
			f.Close()
			return nil, err
		}
		// Trames de taille variable : on releve leurs positions une fois.
		if err := r.indexFrames(); err != nil {
			f.Close()
			return nil, err
		}
	} else {
		// Trames de taille fixe : la position se calcule, sans lire le fichier.
		stride := int64(lenPrefix + frameSize + gcmOvert)
		r.frames = (r.size + frameSize - 1) / frameSize
		if r.frames == 0 {
			r.frames = 1
		}
		_ = stride
	}
	return r, nil
}

func (r *Reader) indexFrames() error {
	offset := int64(headerSize)
	for offset < r.fileSize {
		var prefix [lenPrefix]byte
		if _, err := r.f.ReadAt(prefix[:], offset); err != nil {
			return err
		}
		r.offsets = append(r.offsets, offset)
		offset += lenPrefix + int64(binary.BigEndian.Uint32(prefix[:]))
	}
	r.frames = int64(len(r.offsets))
	return nil
}

// frameAt renvoie la position et la longueur du chiffre d'une trame.
func (r *Reader) frameAt(index int64) (offset int64, length int, err error) {
	if r.compressed {
		if index < 0 || index >= int64(len(r.offsets)) {
			return 0, 0, io.EOF
		}
		offset = r.offsets[index]
	} else {
		offset = headerSize + index*int64(lenPrefix+frameSize+gcmOvert)
		if offset >= r.fileSize {
			return 0, 0, io.EOF
		}
	}
	var prefix [lenPrefix]byte
	if _, err := r.f.ReadAt(prefix[:], offset); err != nil {
		return 0, 0, err
	}
	return offset + lenPrefix, int(binary.BigEndian.Uint32(prefix[:])), nil
}

// loadFrame dechiffre une trame et la place dans le tampon.
func (r *Reader) loadFrame(index int64) error {
	if r.loaded == index {
		return nil
	}
	offset, length, err := r.frameAt(index)
	if err != nil {
		return err
	}
	if cap(r.sealedBuf) < length {
		r.sealedBuf = make([]byte, length)
	}
	sealed := r.sealedBuf[:length]
	if _, err := r.f.ReadAt(sealed, offset); err != nil {
		return err
	}

	if cap(r.plainBuf) < length {
		r.plainBuf = make([]byte, 0, length)
	}

	last := index == r.frames-1
	nonce := frameNonce(r.nonceBase, uint32(index))

	// Le dechiffrement ecrit dans le tampon reutilise, sans nouvelle allocation.
	payload, err := r.gcm.Open(r.plainBuf[:0], nonce, sealed, aadFor(last))
	if err != nil {
		// Une donnee associee differente signale peut-etre une troncature :
		// on retente avec l'autre valeur avant de conclure a une alteration.
		payload, err = r.gcm.Open(r.plainBuf[:0], nonce, sealed, aadFor(!last))
		if err != nil {
			return fmt.Errorf("trame %d alteree ou cle incorrecte", index)
		}
	}
	r.plainBuf = payload[:0]

	if r.compressed {
		if payload, err = r.zr.DecodeAll(payload, nil); err != nil {
			return err
		}
	}
	r.buf = payload
	r.loaded = index
	return nil
}

// Size renvoie la taille en clair du fichier.
func (r *Reader) Size() int64 { return r.size }

func (r *Reader) Read(p []byte) (int, error) {
	if r.plain {
		n, err := r.f.Read(p)
		r.pos += int64(n)
		return n, err
	}
	if r.pos >= r.size {
		return 0, io.EOF
	}

	index := r.pos / frameSize
	if err := r.loadFrame(index); err != nil {
		return 0, err
	}
	within := r.pos - index*frameSize
	if within >= int64(len(r.buf)) {
		return 0, io.EOF
	}

	n := copy(p, r.buf[within:])
	r.pos += int64(n)
	return n, nil
}

// WriteTo permet a io.Copy de vider une trame entiere d'un coup, au lieu de
// recopier par tranches de 32 Kio. C'est ce qui rend le service d'un gros fichier
// comparable a une copie directe.
func (r *Reader) WriteTo(dst io.Writer) (int64, error) {
	if r.plain {
		return io.Copy(dst, r.f)
	}
	var total int64
	for r.pos < r.size {
		index := r.pos / frameSize
		if err := r.loadFrame(index); err != nil {
			if err == io.EOF {
				break
			}
			return total, err
		}
		within := r.pos - index*frameSize
		if within >= int64(len(r.buf)) {
			break
		}
		n, err := dst.Write(r.buf[within:])
		r.pos += int64(n)
		total += int64(n)
		if err != nil {
			return total, err
		}
	}
	return total, nil
}

func (r *Reader) Seek(offset int64, whence int) (int64, error) {
	if r.plain {
		pos, err := r.f.Seek(offset, whence)
		r.pos = pos
		return pos, err
	}
	var target int64
	switch whence {
	case io.SeekStart:
		target = offset
	case io.SeekCurrent:
		target = r.pos + offset
	case io.SeekEnd:
		target = r.size + offset
	default:
		return 0, fmt.Errorf("whence invalide")
	}
	if target < 0 {
		return 0, fmt.Errorf("position negative")
	}
	r.pos = target
	return target, nil
}

func (r *Reader) Close() error {
	if r.zr != nil {
		r.zr.Close()
	}
	return r.f.Close()
}

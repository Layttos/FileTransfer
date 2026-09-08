package vault

import (
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/binary"
	"io"

	"github.com/klauspost/compress/zstd"
)

// Writer chiffre un flux au fil de l'ecriture. La premiere trame est retenue le
// temps de decider si le contenu merite d'etre compresse : l'en-tete n'est ecrit
// qu'ensuite, une fois le drapeau connu.
type Writer struct {
	dst  io.Writer
	buf  []byte // trame en cours
	pend []byte // premiere trame, en attente de la decision

	started    bool
	compressed bool
	index      uint32
	plainSize  uint64

	salt      []byte
	nonceBase []byte
	gcm       cipher.AEAD
	zw        *zstd.Encoder
	err       error
}

// NewWriter enveloppe un writer pour y ecrire un fichier chiffre.
func NewWriter(dst io.Writer) (*Writer, error) {
	salt := make([]byte, 16)
	if _, err := rand.Read(salt); err != nil {
		return nil, err
	}
	nonceBase := make([]byte, 8)
	if _, err := rand.Read(nonceBase); err != nil {
		return nil, err
	}
	key, err := deriveKey(salt)
	if err != nil {
		return nil, err
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	return &Writer{
		dst: dst, salt: salt, nonceBase: nonceBase, gcm: gcm,
		buf: make([]byte, 0, frameSize),
	}, nil
}

// PlainSize renvoie le nombre d'octets en clair ecrits jusqu'ici.
func (w *Writer) PlainSize() int64 { return int64(w.plainSize) }

// Compressed indique si le contenu a ete juge compressible.
func (w *Writer) Compressed() bool { return w.compressed }

func (w *Writer) Write(p []byte) (int, error) {
	if w.err != nil {
		return 0, w.err
	}
	written := len(p)
	w.plainSize += uint64(len(p))

	for len(p) > 0 {
		room := frameSize - len(w.buf)
		if room > len(p) {
			room = len(p)
		}
		w.buf = append(w.buf, p[:room]...)
		p = p[room:]

		if len(w.buf) == frameSize {
			if err := w.flushFrame(false); err != nil {
				w.err = err
				return 0, err
			}
		}
	}
	return written, nil
}

// decide teste la compressibilite sur la premiere trame. Sur un fichier deja
// compresse (video, archive, photo), compresser ne gagne rien et divise le debit
// par sept : on stocke alors brut.
func (w *Writer) decide(first []byte) {
	if len(first) < 4096 {
		w.compressed = false
		return
	}
	var probe bytes.Buffer
	enc, err := zstd.NewWriter(&probe,
		zstd.WithEncoderLevel(zstd.SpeedFastest), zstd.WithEncoderConcurrency(1))
	if err != nil {
		w.compressed = false
		return
	}
	enc.Write(first)
	enc.Close()
	w.compressed = float64(probe.Len()) < float64(len(first))*(1-minCompressionGain)
}

func (w *Writer) writeHeader() error {
	header := make([]byte, headerSize)
	copy(header[0:4], magic[:])
	header[4] = 1
	if w.compressed {
		header[5] |= flagCompressed
	}
	binary.BigEndian.PutUint32(header[8:12], frameSize)
	copy(header[12:28], w.salt)
	copy(header[28:36], w.nonceBase)
	// La taille en clair n'est connue qu'a la fermeture : elle est reecrite alors
	// si le writer le permet, sinon le lecteur la deduit des trames.
	binary.BigEndian.PutUint64(header[36:44], 0)
	_, err := w.dst.Write(header)
	return err
}

func (w *Writer) flushFrame(last bool) error {
	if !w.started {
		w.decide(w.buf)
		if err := w.writeHeader(); err != nil {
			return err
		}
		if w.compressed {
			enc, err := zstd.NewWriter(nil,
				zstd.WithEncoderLevel(zstd.SpeedFastest), zstd.WithEncoderConcurrency(1))
			if err != nil {
				return err
			}
			w.zw = enc
		}
		w.started = true
	}

	payload := w.buf
	if w.compressed {
		payload = w.zw.EncodeAll(w.buf, make([]byte, 0, len(w.buf)))
	}

	sealed := w.gcm.Seal(nil, frameNonce(w.nonceBase, w.index), payload, aadFor(last))

	var prefix [lenPrefix]byte
	binary.BigEndian.PutUint32(prefix[:], uint32(len(sealed)))
	if _, err := w.dst.Write(prefix[:]); err != nil {
		return err
	}
	if _, err := w.dst.Write(sealed); err != nil {
		return err
	}

	w.index++
	w.buf = w.buf[:0]
	return nil
}

// Close scelle la derniere trame et met a jour la taille en clair dans l'en-tete
// si la destination accepte l'ecriture positionnee.
func (w *Writer) Close() error {
	if w.err != nil {
		return w.err
	}
	if err := w.flushFrame(true); err != nil {
		return err
	}
	if w.zw != nil {
		w.zw.Close()
	}

	if seeker, ok := w.dst.(io.WriterAt); ok {
		var size [8]byte
		binary.BigEndian.PutUint64(size[:], w.plainSize)
		if _, err := seeker.WriteAt(size[:], 36); err != nil {
			return err
		}
	}
	return nil
}

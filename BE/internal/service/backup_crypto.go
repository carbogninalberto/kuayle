package service

import (
	"bufio"
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/binary"
	"errors"
	"fmt"
	"io"

	"golang.org/x/crypto/scrypt"
)

// Client-side backup encryption. An encrypted archive is:
//
//	header: "KUAYLEBK1" | salt (16 bytes) | chunk size (uint32, big endian)
//	chunks: AES-256-GCM(plaintext chunk) with a 16-byte tag each
//
// The key is scrypt(passphrase, salt, N=32768, r=8, p=1, 32 bytes). Every
// chunk except the last holds exactly chunk-size plaintext bytes; the last
// holds the rest (possibly none). The 12-byte nonce is 3 zero bytes, the
// chunk counter (uint64, big endian) and a final-chunk flag byte (1 on the
// last chunk), and the header is the additional authenticated data, so
// reordered, dropped or truncated chunks fail to decrypt.
const (
	backupCryptoMagic     = "KUAYLEBK1"
	backupCryptoSaltSize  = 16
	backupCryptoChunkSize = 64 * 1024
	backupCryptoHeaderLen = len(backupCryptoMagic) + backupCryptoSaltSize + 4
	backupScryptN         = 32768
	backupScryptR         = 8
	backupScryptP         = 1
)

// ErrBackupDecrypt reports a wrong passphrase or a damaged encrypted archive.
var ErrBackupDecrypt = errors.New("cannot decrypt the backup: wrong passphrase or damaged file")

func backupStreamCipher(passphrase string, salt []byte) (cipher.AEAD, error) {
	key, err := scrypt.Key([]byte(passphrase), salt, backupScryptN, backupScryptR, backupScryptP, 32)
	if err != nil {
		return nil, err
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	return cipher.NewGCM(block)
}

func backupChunkNonce(counter uint64, final bool) []byte {
	nonce := make([]byte, 12)
	binary.BigEndian.PutUint64(nonce[3:11], counter)
	if final {
		nonce[11] = 1
	}
	return nonce
}

type backupEncryptWriter struct {
	out     io.Writer
	aead    cipher.AEAD
	header  []byte
	buf     []byte
	counter uint64
	closed  bool
}

// newBackupEncryptWriter writes the header to out and returns a writer that
// encrypts everything written to it. Close writes the final chunk; it does
// not close out.
func newBackupEncryptWriter(out io.Writer, passphrase string) (io.WriteCloser, error) {
	salt := make([]byte, backupCryptoSaltSize)
	if _, err := io.ReadFull(rand.Reader, salt); err != nil {
		return nil, err
	}
	aead, err := backupStreamCipher(passphrase, salt)
	if err != nil {
		return nil, err
	}
	header := make([]byte, 0, backupCryptoHeaderLen)
	header = append(header, backupCryptoMagic...)
	header = append(header, salt...)
	header = binary.BigEndian.AppendUint32(header, backupCryptoChunkSize)
	if _, err := out.Write(header); err != nil {
		return nil, err
	}
	return &backupEncryptWriter{out: out, aead: aead, header: header, buf: make([]byte, 0, backupCryptoChunkSize)}, nil
}

func (w *backupEncryptWriter) Write(p []byte) (int, error) {
	if w.closed {
		return 0, errors.New("write to closed backup encrypter")
	}
	written := 0
	for len(p) > 0 {
		// A full buffer is only sealed once more data arrives, so the last
		// chunk is always sealed as final by Close.
		if len(w.buf) == backupCryptoChunkSize {
			if err := w.seal(false); err != nil {
				return written, err
			}
		}
		n := copy(w.buf[len(w.buf):backupCryptoChunkSize], p)
		w.buf = w.buf[:len(w.buf)+n]
		p = p[n:]
		written += n
	}
	return written, nil
}

func (w *backupEncryptWriter) seal(final bool) error {
	sealed := w.aead.Seal(nil, backupChunkNonce(w.counter, final), w.buf, w.header)
	w.counter++
	w.buf = w.buf[:0]
	_, err := w.out.Write(sealed)
	return err
}

func (w *backupEncryptWriter) Close() error {
	if w.closed {
		return nil
	}
	w.closed = true
	return w.seal(true)
}

type backupDecryptReader struct {
	in      *bufio.Reader
	aead    cipher.AEAD
	header  []byte
	chunk   []byte
	plain   []byte
	counter uint64
	done    bool
}

// newBackupDecryptReader reads the header from in and returns a reader of
// the plaintext. Read returns ErrBackupDecrypt for a wrong passphrase or a
// damaged, truncated or extended file.
func newBackupDecryptReader(in io.Reader, passphrase string) (io.Reader, error) {
	header := make([]byte, backupCryptoHeaderLen)
	if _, err := io.ReadFull(in, header); err != nil {
		return nil, fmt.Errorf("reading the encryption header: %w", err)
	}
	if !bytes.Equal(header[:len(backupCryptoMagic)], []byte(backupCryptoMagic)) {
		return nil, errors.New("not an encrypted Kuayle backup")
	}
	chunkSize := binary.BigEndian.Uint32(header[len(backupCryptoMagic)+backupCryptoSaltSize:])
	if chunkSize == 0 || chunkSize > 16<<20 {
		return nil, errors.New("invalid chunk size in the encryption header")
	}
	aead, err := backupStreamCipher(passphrase, header[len(backupCryptoMagic):len(backupCryptoMagic)+backupCryptoSaltSize])
	if err != nil {
		return nil, err
	}
	return &backupDecryptReader{
		in:     bufio.NewReaderSize(in, int(chunkSize)+aead.Overhead()+1),
		aead:   aead,
		header: header,
		chunk:  make([]byte, int(chunkSize)+aead.Overhead()),
	}, nil
}

func (r *backupDecryptReader) Read(p []byte) (int, error) {
	for len(r.plain) == 0 {
		if r.done {
			return 0, io.EOF
		}
		n, err := io.ReadFull(r.in, r.chunk)
		final := false
		switch {
		case errors.Is(err, io.ErrUnexpectedEOF) || errors.Is(err, io.EOF):
			final = true
		case err != nil:
			return 0, err
		default:
			if _, peekErr := r.in.Peek(1); errors.Is(peekErr, io.EOF) {
				final = true
			}
		}
		if n < r.aead.Overhead() {
			return 0, ErrBackupDecrypt
		}
		plain, openErr := r.aead.Open(r.chunk[:0:0], backupChunkNonce(r.counter, final), r.chunk[:n], r.header)
		if openErr != nil {
			return 0, ErrBackupDecrypt
		}
		r.counter++
		r.plain = plain
		r.done = final
	}
	n := copy(p, r.plain)
	r.plain = r.plain[n:]
	return n, nil
}

// DecryptBackup copies the plaintext of an encrypted backup from in to out.
// The server's "backup decrypt" command uses it for restores.
func DecryptBackup(out io.Writer, in io.Reader, passphrase string) error {
	reader, err := newBackupDecryptReader(in, passphrase)
	if err != nil {
		return err
	}
	_, err = io.Copy(out, reader)
	return err
}

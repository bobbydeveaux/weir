// Package archive packs a directory to send to an agent's container, and
// unpacks it there.
package archive

import (
	"archive/tar"
	"compress/gzip"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"strings"
)

// ErrTooLarge is returned when a directory packs to more than the limit.
var ErrTooLarge = errors.New("the working directory is too large to send")

// limited fails once more than max bytes have been written to it.
type limited struct {
	w   io.Writer
	max int64
	n   int64
}

func (l *limited) Write(p []byte) (int, error) {
	if l.n += int64(len(p)); l.max > 0 && l.n > l.max {
		return 0, ErrTooLarge
	}
	return l.w.Write(p)
}

// Pack writes dir to w as a compressed archive of at most max bytes. Ordinary
// files, directories and links are kept; anything else is left out.
func Pack(dir string, w io.Writer, max int64) error {
	zip := gzip.NewWriter(&limited{w: w, max: max})
	out := tar.NewWriter(zip)
	err := filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(dir, p)
		if err != nil || rel == "." {
			return err
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		name := filepath.ToSlash(rel)
		switch {
		case d.IsDir():
			return out.WriteHeader(&tar.Header{Name: name + "/", Typeflag: tar.TypeDir, Mode: int64(info.Mode().Perm())})
		case d.Type()&fs.ModeSymlink != 0:
			target, err := os.Readlink(p)
			if err != nil {
				return err
			}
			return out.WriteHeader(&tar.Header{Name: name, Typeflag: tar.TypeSymlink, Linkname: target, Mode: 0o777})
		case d.Type().IsRegular():
			if err := out.WriteHeader(&tar.Header{Name: name, Typeflag: tar.TypeReg, Mode: int64(info.Mode().Perm()), Size: info.Size()}); err != nil {
				return err
			}
			file, err := os.Open(p)
			if err != nil {
				return err
			}
			defer file.Close()
			_, err = io.Copy(out, file)
			return err
		}
		return nil
	})
	if err != nil {
		return err
	}
	if err := out.Close(); err != nil {
		return err
	}
	return zip.Close()
}

// Unpack reads an archive made by Pack into dir. A name that would land
// outside dir is refused.
func Unpack(r io.Reader, dir string) error {
	zip, err := gzip.NewReader(r)
	if err != nil {
		return fmt.Errorf("the archive could not be read: %w", err)
	}
	in := tar.NewReader(zip)
	for {
		h, err := in.Next()
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return fmt.Errorf("the archive could not be read: %w", err)
		}
		clean := path.Clean(h.Name)
		if clean == "." {
			continue
		}
		if strings.HasPrefix(clean, "/") || clean == ".." || strings.HasPrefix(clean, "../") {
			return fmt.Errorf("the archive holds %q, which is outside the directory", h.Name)
		}
		target := filepath.Join(dir, filepath.FromSlash(clean))
		mode := fs.FileMode(h.Mode).Perm()
		switch h.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(target, mode|0o700); err != nil {
				return err
			}
		case tar.TypeSymlink:
			if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
				return err
			}
			if err := os.Symlink(h.Linkname, target); err != nil {
				return err
			}
		case tar.TypeReg:
			if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
				return err
			}
			file, err := os.OpenFile(target, os.O_CREATE|os.O_WRONLY|os.O_EXCL, mode|0o600)
			if err != nil {
				return err
			}
			if _, err := io.Copy(file, in); err != nil {
				file.Close()
				return err
			}
			if err := file.Close(); err != nil {
				return err
			}
		}
	}
}

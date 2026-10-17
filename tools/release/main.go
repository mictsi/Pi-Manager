// Command release packages fresh cross-platform builds with documentation,
// license notices and checksums. It never reads a user's runtime configuration.
package main

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"crypto/sha256"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

type entry struct {
	source string
	name   string
}

func main() {
	input := flag.String("input", "", "Fresh build directory from build.sh")
	output := flag.String("output", "dist", "Archive destination")
	version := flag.String("version", "", "Release version X.Y.Z")
	flag.Parse()
	if err := release(*input, *output, *version); err != nil {
		fmt.Fprintln(os.Stderr, "release:", err)
		os.Exit(1)
	}
}

func release(input, output, version string) error {
	if input == "" || !regexp.MustCompile(`^(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)$`).MatchString(version) {
		return fmt.Errorf("provide a fresh build directory and version X.Y.Z")
	}
	if err := os.MkdirAll(output, 0755); err != nil {
		return err
	}
	var checksums strings.Builder
	for _, platform := range []string{"windows", "linux", "darwin"} {
		for _, arch := range []string{"amd64", "arm64"} {
			base := "pi-manager_" + version + "_" + platform + "_" + arch
			root := filepath.Join(input, platform+"-"+arch)
			var entries []entry
			err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
				if err != nil {
					return err
				}
				if d.IsDir() {
					return nil
				}
				name, err := filepath.Rel(root, path)
				if err != nil {
					return err
				}
				entries = append(entries, entry{path, filepath.ToSlash(filepath.Join(base, name))})
				return nil
			})
			if err != nil {
				return err
			}
			for _, name := range []string{"README.md", "LICENSE", "config.example.json", "assets/THIRD_PARTY.txt", "assets/icon.svg"} {
				entries = append(entries, entry{name, base + "/" + name})
			}
			ext := ".tar.gz"
			if platform == "windows" {
				ext = ".zip"
			}
			name := base + ext
			file, err := os.Create(filepath.Join(output, name))
			if err != nil {
				return err
			}
			hash := sha256.New()
			err = archive(io.MultiWriter(file, hash), entries, platform == "windows")
			closeErr := file.Close()
			if err != nil {
				return err
			}
			if closeErr != nil {
				return closeErr
			}
			fmt.Fprintf(&checksums, "%x  %s\n", hash.Sum(nil), name)
		}
	}
	return os.WriteFile(filepath.Join(output, "SHA256SUMS"), []byte(checksums.String()), 0644)
}

func archive(output io.Writer, entries []entry, windows bool) error {
	var add func(os.FileInfo, string) (io.Writer, error)
	var closeArchive func() error
	if windows {
		writer := zip.NewWriter(output)
		add = func(info os.FileInfo, name string) (io.Writer, error) {
			header, err := zip.FileInfoHeader(info)
			if err != nil {
				return nil, err
			}
			header.Name, header.Method = name, zip.Deflate
			return writer.CreateHeader(header)
		}
		closeArchive = writer.Close
	} else {
		compressed := gzip.NewWriter(output)
		writer := tar.NewWriter(compressed)
		add = func(info os.FileInfo, name string) (io.Writer, error) {
			header, err := tar.FileInfoHeader(info, "")
			if err != nil {
				return nil, err
			}
			header.Name = name
			return writer, writer.WriteHeader(header)
		}
		closeArchive = func() error {
			err := writer.Close()
			zipErr := compressed.Close()
			if err != nil {
				return err
			}
			return zipErr
		}
	}
	for _, item := range entries {
		if err := addFile(item, add); err != nil {
			return err
		}
	}
	return closeArchive()
}

func addFile(item entry, add func(os.FileInfo, string) (io.Writer, error)) error {
	file, err := os.Open(item.source)
	if err != nil {
		return err
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("release input is not a regular file: %s", item.source)
	}
	writer, err := add(info, item.name)
	if err != nil {
		return err
	}
	_, err = io.Copy(writer, file)
	return err
}

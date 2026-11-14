package main

import (
	"archive/zip"
	"context"
	"crypto/sha256"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

func distribution(ctx context.Context, root string) error {
	for _, key := range []string{"GOOS", "GOARCH"} {
		expected := runtime.GOOS
		if key == "GOARCH" {
			expected = runtime.GOARCH
		}
		if value := os.Getenv(key); value != "" && value != expected {
			return fmt.Errorf("main: dist builds for this host; unset %s=%s first (CI handles other platforms)", key, value)
		}
	}
	out := filepath.Join(root, "dist")
	err := os.MkdirAll(out, 0755)
	if err != nil {
		return err
	}
	info, err := os.Lstat(out)
	if err != nil {
		return err
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("main: dist must be a real directory")
	}
	name := project + "-" + runtime.GOOS + "-" + runtime.GOARCH
	destination := filepath.Join(out, name)
	err = checkDestination(destination)
	if err != nil {
		return err
	}
	temporary, err := os.MkdirTemp(out, ".stage-")
	if err != nil {
		return err
	}
	defer func() { _ = os.RemoveAll(temporary) }() // Unpublished staging files are disposable.

	fmt.Println("[1/3] Build native release binaries")
	for _, target := range applicationTargets() {
		err = buildBinary(ctx, target, filepath.Join(temporary, executable(target)), nil)
		if err != nil {
			return err
		}
	}
	fmt.Println("[2/3] Stage release documentation, example config, launchers and assets")
	err = copyPayload(ctx, root, temporary)
	if err != nil {
		return err
	}
	err = os.WriteFile(filepath.Join(temporary, ".build-dist"), []byte(project+"\n"), 0644)
	if err != nil {
		return err
	}
	fmt.Println("[3/3] Create and verify ZIP; write SHA-256 checksum")
	archive := filepath.Join(out, name+".zip")
	err = writeArchive(temporary, name, archive)
	if err != nil {
		return err
	}
	err = os.RemoveAll(destination)
	if err != nil {
		return err
	}
	err = os.Rename(temporary, destination)
	if err != nil {
		return err
	}
	fmt.Println("Folder:  ", destination)
	fmt.Println("Archive: ", archive)
	fmt.Println("Checksum:", archive+".sha256")
	fmt.Println("Package created from the current checkout. Nothing was published.")
	return nil
}

func checkDestination(destination string) error {
	info, err := os.Lstat(destination)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("main: refusing to replace non-directory or symlink %s", destination)
	}
	marker, err := os.ReadFile(filepath.Join(destination, ".build-dist"))
	if err != nil || string(marker) != project+"\n" {
		return fmt.Errorf("main: %s is not a distribution owned by this helper; move it aside before retrying", destination)
	}
	return nil
}

func writeArchive(directory, name, destination string) error {
	temporary, err := os.CreateTemp(filepath.Dir(destination), ".archive-*.zip")
	if err != nil {
		return err
	}
	tempName := temporary.Name()
	defer func() { _ = os.Remove(tempName) }() // Completed archives are renamed out of this path.
	writer := zip.NewWriter(temporary)
	err = filepath.WalkDir(directory, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if path == directory {
			return nil
		}
		return archiveFile(writer, directory, name, path)
	})
	closeErr := writer.Close()
	fileErr := temporary.Close()
	if err != nil {
		return err
	}
	if closeErr != nil {
		return closeErr
	}
	if fileErr != nil {
		return fileErr
	}
	err = verifyArchive(tempName, directory, name)
	if err != nil {
		return err
	}
	digest, err := fileDigest(tempName)
	if err != nil {
		return err
	}
	checksum := fmt.Sprintf("%x  %s\n", digest, filepath.Base(destination))
	err = os.Rename(tempName, destination)
	if err != nil {
		return err
	}
	return os.WriteFile(destination+".sha256", []byte(checksum), 0644)
}

func archiveFile(writer *zip.Writer, directory, name, path string) error {
	relative, err := filepath.Rel(directory, path)
	if err != nil {
		return err
	}
	info, err := os.Lstat(path)
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() && !info.IsDir() {
		return fmt.Errorf("main: archive payload must be a regular file: %s", path)
	}
	header, err := zip.FileInfoHeader(info)
	if err != nil {
		return err
	}
	header.Name = name + "/" + filepath.ToSlash(relative)
	if info.IsDir() {
		header.Name += "/"
		header.SetMode(os.ModeDir | 0755)
		_, err = writer.CreateHeader(header)
		return err
	}
	header.Method = zip.Deflate
	mode := os.FileMode(0644)
	if strings.HasSuffix(relative, ".sh") || strings.HasSuffix(relative, ".exe") || !strings.Contains(relative, ".") {
		mode = 0755
	}
	header.SetMode(mode)
	member, err := writer.CreateHeader(header)
	if err != nil {
		return err
	}
	file, err := os.Open(path)
	if err != nil {
		return err
	}
	defer file.Close()
	_, err = io.Copy(member, file)
	return err
}

func verifyArchive(archive, directory, name string) error {
	reader, err := zip.OpenReader(archive)
	if err != nil {
		return err
	}
	defer func() { _ = reader.Close() }() // Read-only ZIP cleanup has no result to publish.
	seen := make(map[string]bool)
	for _, member := range reader.File {
		relative := strings.TrimPrefix(member.Name, name+"/")
		if relative == member.Name || !filepath.IsLocal(filepath.FromSlash(relative)) || seen[relative] {
			return fmt.Errorf("main: unexpected or duplicate archive path: %s", member.Name)
		}
		seen[relative] = true
		path := filepath.Join(directory, filepath.FromSlash(relative))
		if member.FileInfo().IsDir() {
			info, statErr := os.Stat(path)
			if statErr != nil {
				return statErr
			}
			if !info.IsDir() {
				return fmt.Errorf("main: archive directory differs: %s", member.Name)
			}
			continue
		}
		err = verifyMember(member, path)
		if err != nil {
			return err
		}
	}
	return filepath.WalkDir(directory, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil || path == directory {
			return walkErr
		}
		relative, relErr := filepath.Rel(directory, path)
		if relErr != nil {
			return relErr
		}
		key := filepath.ToSlash(relative)
		if entry.IsDir() {
			key += "/"
		}
		if !seen[key] {
			return fmt.Errorf("main: archive is missing %s", key)
		}
		return nil
	})
}

func fileDigest(path string) ([]byte, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	hash := sha256.New()
	_, err = io.Copy(hash, file)
	if err != nil {
		return nil, err
	}
	return hash.Sum(nil), nil
}

func verifyMember(member *zip.File, path string) error {
	stream, err := member.Open()
	if err != nil {
		return err
	}
	defer stream.Close()
	hash := sha256.New()
	_, err = io.Copy(hash, stream)
	if err != nil {
		return err
	}
	source, err := fileDigest(path)
	if err != nil {
		return err
	}
	if string(hash.Sum(nil)) != string(source) {
		return fmt.Errorf("main: archive contents differ: %s", member.Name)
	}
	return nil
}

func copyPayload(ctx context.Context, root, destination string) error {
	_, err := exec.LookPath("node")
	if err != nil {
		return fmt.Errorf("main: Node.js is required to bundle Melodix documentation: %w", err)
	}
	return command(ctx, nil, "node", "scripts/archive-docs.mjs", "--destination", destination)
}

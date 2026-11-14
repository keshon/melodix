package main

import (
	"archive/zip"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func TestRunArguments(t *testing.T) {
	target := applicationTargets()[0]
	got, args, err := runArguments([]string{target, "--", "-flag", "value with spaces"})
	if err != nil || got != target || !slices.Equal(args, []string{"-flag", "value with spaces"}) {
		t.Fatalf("target=%q args=%q err=%v", got, args, err)
	}
	got, args, err = runArguments([]string{"--", "-help"})
	if err != nil || got != target || !slices.Equal(args, []string{"-help"}) {
		t.Fatalf("default target=%q args=%q err=%v", got, args, err)
	}
	_, _, err = runArguments([]string{"missing-target"})
	if err == nil {
		t.Fatal("unknown target accepted")
	}
	err = run(context.Background(), []string{"dist", "unexpected"})
	if err == nil {
		t.Fatal("dist accepted unused arguments")
	}
}

func TestArchivePreservesPayloadAndEmptyDirectories(t *testing.T) {
	directory := t.TempDir()
	err := os.Mkdir(filepath.Join(directory, "data"), 0755)
	if err != nil {
		t.Fatal(err)
	}
	err = os.WriteFile(filepath.Join(directory, "start.sh"), []byte("#!/bin/sh\necho ready\n"), 0755)
	if err != nil {
		t.Fatal(err)
	}
	archive := filepath.Join(t.TempDir(), "fixture.zip")
	err = writeArchive(directory, "fixture", archive)
	if err != nil {
		t.Fatal(err)
	}
	reader, err := zip.OpenReader(archive)
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	for _, member := range reader.File {
		if member.Name == "fixture/start.sh" && member.Mode().Perm() != 0755 {
			t.Fatalf("launcher permissions = %v", member.Mode())
		}
	}
	digest, err := fileDigest(archive)
	if err != nil {
		t.Fatal(err)
	}
	checksum, err := os.ReadFile(archive + ".sha256")
	if err != nil {
		t.Fatal(err)
	}
	if string(checksum) != fmt.Sprintf("%x  fixture.zip\n", digest) {
		t.Fatalf("wrong checksum: %s", checksum)
	}
	err = os.WriteFile(filepath.Join(directory, "start.sh"), []byte("changed"), 0755)
	if err != nil {
		t.Fatal(err)
	}
	err = verifyArchive(archive, directory, "fixture")
	if err == nil {
		t.Fatal("changed payload was not detected")
	}
}

func TestArchiveRejectsMissingDuplicateAndUnsafeMembers(t *testing.T) {
	for _, names := range [][]string{{}, {"fixture/file", "fixture/file"}, {"fixture/../outside"}} {
		t.Run(strings.Join(names, ","), func(t *testing.T) {
			directory := t.TempDir()
			err := os.WriteFile(filepath.Join(directory, "file"), []byte("payload"), 0644)
			if err != nil {
				t.Fatal(err)
			}
			archive := filepath.Join(t.TempDir(), "fixture.zip")
			file, err := os.Create(archive)
			if err != nil {
				t.Fatal(err)
			}
			writer := zip.NewWriter(file)
			for _, name := range names {
				member, createErr := writer.Create(name)
				if createErr != nil {
					t.Fatal(createErr)
				}
				_, err = member.Write([]byte("payload"))
				if err != nil {
					t.Fatal(err)
				}
			}
			err = writer.Close()
			if err != nil {
				t.Fatal(err)
			}
			err = file.Close()
			if err != nil {
				t.Fatal(err)
			}
			err = verifyArchive(archive, directory, "fixture")
			if err == nil {
				t.Fatal("invalid archive accepted")
			}
		})
	}
}

func TestDistributionDoesNotReplaceUnownedDirectory(t *testing.T) {
	directory := t.TempDir()
	err := checkDestination(directory)
	if err == nil {
		t.Fatal("unowned directory accepted")
	}
	err = os.WriteFile(filepath.Join(directory, ".build-dist"), []byte(project+"\n"), 0644)
	if err != nil {
		t.Fatal(err)
	}
	err = checkDestination(directory)
	if err != nil {
		t.Fatal(err)
	}
}

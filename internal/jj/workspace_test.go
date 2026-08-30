package jj

import (
	"os"
	"strings"
	"testing"
)

func TestWorkspaceCommands(t *testing.T) {
	jjPath, argsFile := recordArgsJJ(t)
	r := NewRunner(Config{JJPath: jjPath, RepoRoot: t.TempDir()})

	if err := r.WorkspaceAdd("-next tree", "next", "main", "dev"); err != nil {
		t.Fatal(err)
	}
	args, err := os.ReadFile(argsFile)
	if err != nil {
		t.Fatal(err)
	}
	want := "workspace\nadd\n--name\nnext\n-r\nmain\n-r\ndev\n--\n-next tree\n"
	if string(args) != want {
		t.Fatalf("WorkspaceAdd argv:\n%s\nwant:\n%s", args, want)
	}

	if err := r.WorkspaceForget("-old"); err != nil {
		t.Fatal(err)
	}
	args, _ = os.ReadFile(argsFile)
	if string(args) != "workspace\nforget\n--\n-old\n" {
		t.Fatalf("WorkspaceForget argv: %q", args)
	}

	if err := r.WorkspaceRename("-renamed"); err != nil {
		t.Fatal(err)
	}
	args, _ = os.ReadFile(argsFile)
	if string(args) != "workspace\nrename\n--\n-renamed\n" {
		t.Fatalf("WorkspaceRename argv: %q", args)
	}

	if err := r.WorkspaceUpdateStale(); err != nil {
		t.Fatal(err)
	}
	args, _ = os.ReadFile(argsFile)
	if string(args) != "workspace\nupdate-stale\n" {
		t.Fatalf("WorkspaceUpdateStale argv: %q", args)
	}
}

func TestWorkspaceCommandValidation(t *testing.T) {
	r := NewRunner(Config{})
	if err := r.WorkspaceAdd("", ""); err == nil {
		t.Fatal("WorkspaceAdd accepted an empty destination")
	}
	if err := r.WorkspaceForget(); err == nil {
		t.Fatal("WorkspaceForget accepted no names")
	}
	if err := r.WorkspaceRename(""); err == nil {
		t.Fatal("WorkspaceRename accepted an empty name")
	}
}

func TestWorkspaceList(t *testing.T) {
	dir := t.TempDir()
	argsFile := dir + "/args"
	jjPath := dir + "/jj"
	body := "#!/bin/sh\nprintf '%s\\n' \"$@\" > " + argsFile + "\nprintf '%s\\n' '{\"name\":\"default\",\"root\":\"/tmp/a b\",\"change_id\":\"abc\",\"commit_id\":\"def\"}' '{\"name\":\"gone\",\"root\":null,\"change_id\":\"ghi\",\"commit_id\":\"jkl\"}'\n"
	if err := os.WriteFile(jjPath, []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}
	r := NewRunner(Config{JJPath: jjPath, RepoRoot: dir})
	got, err := r.WorkspaceList()
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0].Root != "/tmp/a b" || got[1].Root != "" {
		t.Fatalf("WorkspaceList = %+v", got)
	}
	args, _ := os.ReadFile(argsFile)
	if !strings.HasPrefix(string(args), "workspace\nlist\n--ignore-working-copy\n--color\nnever\n-T\n") {
		t.Fatalf("WorkspaceList argv: %q", args)
	}
}

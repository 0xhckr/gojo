// bump: set the project version in VERSION + update the nix vendorHash.
//
// Usage:
//
//	go run scripts/bump.go            # bump patch (1.2.2 -> 1.2.3)
//	go run scripts/bump.go major      # 1.2.2 -> 2.0.0
//	go run scripts/bump.go minor      # 1.2.2 -> 1.3.0
//	go run scripts/bump.go patch      # 1.2.2 -> 1.2.3
//	go run scripts/bump.go 1.2.2      # set a specific version
//	nix run .#bump -- [major|minor|patch|X.Y.Z]   (same, via the flake app)
//
// Leading `v` is stripped.  After writing VERSION the script runs
// `nix build .#default` and, if the vendorHash is stale, captures the
// "got:" hash and patches flake.nix in place.
package main

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
)

func main() {
	var version string

	readCurrent := func() string {
		old, err := os.ReadFile("VERSION")
		if err != nil {
			die("reading VERSION: %v", err)
		}
		return strings.TrimSpace(string(old))
	}

	switch len(os.Args) {
	case 1:
		version = bumpPart(readCurrent(), "patch")
	case 2:
		switch arg := strings.TrimPrefix(os.Args[1], "v"); arg {
		case "major", "minor", "patch":
			version = bumpPart(readCurrent(), arg)
		default:
			version = arg
		}
	default:
		die("usage: go run scripts/bump.go [major|minor|patch|version]")
	}

	if !semverRE.MatchString(version) {
		die("invalid version %q (want X.Y.Z)", version)
	}

	// 1. Write VERSION
	if err := os.WriteFile("VERSION", []byte(version+"\n"), 0o644); err != nil {
		die("writing VERSION: %v", err)
	}
	fmt.Printf("VERSION -> %s\n", version)

	// 2. Update vendorHash in flake.nix if nix build says it's stale
	updateVendorHash()

	fmt.Println("\nDone. Next:")
	fmt.Printf("  git add VERSION flake.nix && git commit -m \"chore: release v%s\"\n", version)
	fmt.Printf("  git tag v%s && git push origin v%s\n", version, version)
}

var semverRE = regexp.MustCompile(`^\d+\.\d+\.\d+$`)

// bumpPart bumps the given part ("major", "minor", "patch") of an X.Y.Z
// version, resetting lower parts per semantic versioning.
func bumpPart(v, part string) string {
	parts := strings.Split(v, ".")
	if len(parts) != 3 {
		die("can't bump %q — want X.Y.Z, pass an explicit version", v)
	}
	nums := [3]int{}
	for i, p := range parts {
		n, err := strconv.Atoi(p)
		if err != nil {
			die("bad number in %q: %v", v, err)
		}
		nums[i] = n
	}
	switch part {
	case "major":
		nums[0]++
		nums[1], nums[2] = 0, 0
	case "minor":
		nums[1]++
		nums[2] = 0
	case "patch":
		nums[2]++
	default:
		die("unknown bump part %q (want major|minor|patch)", part)
	}
	return fmt.Sprintf("%d.%d.%d", nums[0], nums[1], nums[2])
}

var hashRE = regexp.MustCompile(`got:\s*(sha256-[A-Za-z0-9+/=]+)`)

func updateVendorHash() {
	fmt.Print("nix build .#default ... ")
	cmd := exec.Command("nix", "build", ".#default", "--no-link")
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err == nil {
		fmt.Println("OK (vendorHash unchanged)")
		return
	}

	out := stderr.String()
	m := hashRE.FindStringSubmatch(out)
	if m == nil {
		fmt.Println("\nnix build failed (not a vendorHash mismatch — see below):")
		fmt.Print(out)
		return
	}
	got := m[1]

	flake, err := os.ReadFile("flake.nix")
	if err != nil {
		die("reading flake.nix: %v", err)
	}
	old := `vendorHash = "sha256-K81au2jpYoRcKvGIGwnwXkXLpPK7NBfuLxb9PinC6VM=";`
	replaced := strings.Replace(string(flake), old, fmt.Sprintf(`vendorHash = "%s";`, got), 1)
	if replaced == string(flake) {
		// Fallback: replace any vendorHash line.
		re := regexp.MustCompile(`vendorHash = "sha256-[A-Za-z0-9+/=]+";`)
		replaced = re.ReplaceAllString(string(flake), fmt.Sprintf(`vendorHash = "%s";`, got))
	}
	if err := os.WriteFile("flake.nix", []byte(replaced), 0o644); err != nil {
		die("writing flake.nix: %v", err)
	}
	fmt.Printf("updated vendorHash -> %s\n", got)

	// Rebuild to confirm.
	fmt.Print("nix build .#default (verify) ... ")
	cmd2 := exec.Command("nix", "build", ".#default", "--no-link")
	cmd2.Stderr = os.Stderr
	if err := cmd2.Run(); err != nil {
		die("verification build failed", )
	}
	fmt.Println("OK")
}

func die(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "bump: "+format+"\n", args...)
	os.Exit(1)
}
